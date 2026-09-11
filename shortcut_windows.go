//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// createNoWindow 对应 Win32 的 CREATE_NO_WINDOW，
// 避免从 GUI 进程拉起 PowerShell 时闪出一个控制台窗口。
const createNoWindow = 0x08000000

// createShortcuts 按选项创建桌面与开始菜单快捷方式。
// 借系统自带的 WScript.Shell 写 .lnk，避免自行实现 .lnk 的二进制格式。
func createShortcuts(installDir string, opts installOptions) error {
	type shortcutTarget struct {
		folder string // PowerShell 的已知文件夹名
		name   string
	}
	var targets []shortcutTarget
	if opts.Desktop {
		targets = append(targets, shortcutTarget{"Desktop", "KfuPet"})
	}
	if opts.StartMenu {
		targets = append(targets, shortcutTarget{"Programs", "KfuPet"})
	}
	if len(targets) == 0 {
		return nil
	}

	var script strings.Builder
	script.WriteString("$ErrorActionPreference = 'Stop'\n")
	script.WriteString("$shell = New-Object -ComObject WScript.Shell\n")
	for _, t := range targets {
		// 用 GetFolderPath 解析，兼容桌面被重定向到 OneDrive 等情况。
		fmt.Fprintf(&script, "$dir = [Environment]::GetFolderPath('%s')\n", t.folder)
		fmt.Fprintf(&script, "$lnk = $shell.CreateShortcut((Join-Path $dir '%s.lnk'))\n", t.name)
		script.WriteString("$lnk.TargetPath = $env:KFUPET_TARGET\n")
		script.WriteString("$lnk.WorkingDirectory = $env:KFUPET_WORKDIR\n")
		script.WriteString("$lnk.Save()\n")
	}

	cmd := exec.Command("powershell",
		"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-Command", script.String())
	// 目标路径走环境变量传入，避免拼脚本时的引号转义问题。
	cmd.Env = append(os.Environ(),
		"KFUPET_TARGET="+filepath.Join(installDir, executableName),
		"KFUPET_WORKDIR="+installDir,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}

	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("创建快捷方式失败：%v（%s）", err, strings.TrimSpace(string(out)))
	}
	return nil
}
