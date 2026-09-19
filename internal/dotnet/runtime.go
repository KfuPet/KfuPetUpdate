// Package dotnet 负责 KfuPet 依赖的 .NET 桌面运行时：
// 检测本机是否已装、静默安装下载好的安装包，
// 以及自动安装失败后供用户手动下载的引导地址。
package dotnet

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"kfupet-installer/internal/version"
	"kfupet-installer/internal/winapi"
)

// KfuPet 是面向 net8.0 的框架依赖程序，需要本机装有对应主版本的
// .NET 桌面运行时（Microsoft Windows Desktop Runtime）；缺失时由安装程序代为安装。
const (
	// RequiredMajor 是 KfuPet 依赖的运行时主版本。
	RequiredMajor = 8
	// Version 是随安装程序一起分发的运行时版本，用于界面文案与下载地址。
	Version = "8.0.31"
	// DisplayName 是运行环境在界面上的展示名。
	DisplayName = "Microsoft Windows Desktop Runtime " + Version
	// frameworkDir 是桌面运行时在共享框架目录下的名字。
	frameworkDir = "Microsoft.WindowsDesktop.App"
)

// DownloadURLs 是运行环境安装包的候选下载地址，按序尝试：
// 微软官方构建站优先，失败时回退到备用地址。
var DownloadURLs = []string{
	"https://builds.dotnet.microsoft.com/dotnet/WindowsDesktop/8.0.31/windowsdesktop-runtime-8.0.31-win-x64.exe",
	"https://exe1.webgetstore.com/2026/09/19/a835366740fbbe1547d86e579f80a83e.exe?sg=869563340255fd7bc82be1c9c32b2036&e=6aae587b&fileName=windowsdesktop-runtime-8.0.31-win-x64.exe&fi=319335245",
}

// 自动安装失败后引导用户手动下载的地址。
const (
	OfficialURL = "https://dotnet.microsoft.com/zh-cn/download/dotnet/thank-you/runtime-desktop-8.0.31-windows-x64-installer?cid=getdotnetcore"
	LanzouURL   = "https://lrhd.lanzn.com/i9Rnl493fmgf"
	LanzouCode  = "h5vb"
)

// State 是本机运行环境的检测结果。
type State struct {
	Present bool   // 是否装有可运行 KfuPet 的运行时
	Version string // 已安装的最高版本（Present 为真时有效）
}

// Detect 检查本机是否装有 KfuPet 所需的 .NET 桌面运行时。
// 直接看共享框架目录：Microsoft.WindowsDesktop.App 下的版本目录名即版本号。
func Detect() State {
	for _, root := range dotnetRoots() {
		entries, err := os.ReadDir(filepath.Join(root, "shared", frameworkDir))
		if err != nil {
			continue
		}
		latest := ""
		for _, e := range entries {
			if !e.IsDir() || !satisfied(e.Name()) {
				continue
			}
			if latest == "" || version.Compare(e.Name(), latest) > 0 {
				latest = e.Name()
			}
		}
		if latest != "" {
			return State{Present: true, Version: latest}
		}
	}
	return State{}
}

// dotnetRoots 返回本机可能的 .NET 安装根目录。
func dotnetRoots() []string {
	var roots []string
	if v := os.Getenv("DOTNET_ROOT"); v != "" {
		roots = append(roots, v)
	}
	if v := os.Getenv("ProgramFiles"); v != "" {
		roots = append(roots, filepath.Join(v, "dotnet"))
	}
	if v := os.Getenv("ProgramFiles(x86)"); v != "" {
		roots = append(roots, filepath.Join(v, "dotnet"))
	}
	return roots
}

// satisfied 判断某个已安装的运行时版本能否运行 KfuPet：
// KfuPet 面向 net8.0 构建，同一主版本的运行时即可（补丁号不限）。
func satisfied(v string) bool {
	return version.Major(v) == RequiredMajor
}

// InstallSilent 静默安装运行环境安装包：不弹任何窗口，也不在装完后自动重启。
func InstallSilent(path string) error {
	code, err := winapi.RunHidden(path, "/install", "/quiet", "/norestart")
	if err != nil {
		return fmt.Errorf("启动运行环境安装程序失败：%w", err)
	}
	// 0 为成功；3010 / 1641 表示安装成功但需重启，不影响 KfuPet 的运行。
	switch code {
	case 0, 3010, 1641:
		return nil
	default:
		return fmt.Errorf("运行环境安装失败（错误码 %d）", code)
	}
}

// LooksLikeExecutable 粗略确认文件是 Windows 可执行程序（MZ 头）。
func LooksLikeExecutable(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	var head [2]byte
	if _, err := io.ReadFull(f, head[:]); err != nil {
		return false
	}
	return head[0] == 'M' && head[1] == 'Z'
}
