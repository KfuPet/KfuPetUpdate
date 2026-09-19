//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"kfupet-installer/internal/winapi"
)

// shortcutName 是快捷方式的文件名（不含 .lnk）。
const shortcutName = "KfuPet"

// shortcutTarget 描述一个快捷方式落点。
// 装在 %ProgramFiles%（机装）的程序，快捷方式也应建在"所有用户"目录下：
// 只给当前用户的话，多用户机器上其他用户看不到入口，与"机装"名不副实。
type shortcutTarget struct {
	folder string // [Environment]::GetFolderPath 的文件夹名
	sub    string // 文件夹下还需再进的子目录（公共开始菜单的 Programs）
}

// 安装时使用的公共落点：所有用户可见。
var (
	commonDesktop   = shortcutTarget{folder: "CommonDesktopDirectory"}
	commonStartMenu = shortcutTarget{folder: "CommonStartMenu", sub: "Programs"}
)

// allShortcutTargets 覆盖新旧两代落点：卸载时都要清。
// 当前用户那两处是早期版本建的，留着会变成指向已删程序的死链接。
var allShortcutTargets = []shortcutTarget{
	commonDesktop, commonStartMenu,
	{folder: "Desktop"}, {folder: "Programs"},
}

// runPowerShell 以隐藏窗口的方式执行一段 PowerShell 脚本。
// 数据经环境变量传入，避免拼接脚本时出现引号转义问题。
func runPowerShell(script string, extraEnv []string) error {
	cmd := exec.Command("powershell",
		"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-Command", script)
	cmd.Env = append(os.Environ(), extraEnv...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: winapi.CreateNoWindow}

	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%v（%s）", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// createShortcuts 按选项创建桌面与开始菜单快捷方式。
// 借系统自带的 WScript.Shell 写 .lnk，避免自行实现 .lnk 的二进制格式。
func createShortcuts(installDir string, opts installOptions) error {
	var targets []shortcutTarget
	if opts.Desktop {
		targets = append(targets, commonDesktop)
	}
	if opts.StartMenu {
		targets = append(targets, commonStartMenu)
	}
	if len(targets) == 0 {
		return nil
	}

	var script strings.Builder
	script.WriteString("$ErrorActionPreference = 'Stop'\n")
	script.WriteString("$shell = New-Object -ComObject WScript.Shell\n")
	for _, t := range targets {
		writeDirResolve(&script, t)
		fmt.Fprintf(&script, "$lnk = $shell.CreateShortcut((Join-Path $dir '%s.lnk'))\n", shortcutName)
		script.WriteString("$lnk.TargetPath = $env:KFUPET_TARGET\n")
		script.WriteString("$lnk.WorkingDirectory = $env:KFUPET_WORKDIR\n")
		script.WriteString("$lnk.Save()\n")
	}

	env := []string{
		"KFUPET_TARGET=" + filepath.Join(installDir, executableName),
		"KFUPET_WORKDIR=" + installDir,
	}
	if err := runPowerShell(script.String(), env); err != nil {
		return fmt.Errorf("创建快捷方式失败：%w", err)
	}
	return nil
}

// removeShortcuts 删除安装时可能创建过的快捷方式（公共与当前用户两处）。
// 快捷方式本就不存在不算错误。
func removeShortcuts() error {
	var script strings.Builder
	script.WriteString("$ErrorActionPreference = 'Stop'\n")
	for _, t := range allShortcutTargets {
		writeDirResolve(&script, t)
		// 取不到目录就跳过：删除是尽力而为，不该因此判定卸载失败。
		script.WriteString("if ($dir) {\n")
		fmt.Fprintf(&script, "$lnk = Join-Path $dir '%s.lnk'\n", shortcutName)
		script.WriteString("if (Test-Path -LiteralPath $lnk) { Remove-Item -LiteralPath $lnk -Force }\n")
		script.WriteString("}\n")
	}

	if err := runPowerShell(script.String(), nil); err != nil {
		return fmt.Errorf("删除快捷方式失败：%w", err)
	}
	return nil
}

// writeDirResolve 生成解析落点目录的 PowerShell 片段，结果放在 $dir。
// 用 GetFolderPath 解析，兼容桌面被重定向到别的盘（如 OneDrive）等情况。
// 落点带子目录（公共开始菜单的 Programs）时先取基础目录，取不到就把 $dir 置空，
// 由调用方按空目录处理，避免 Join-Path 收到空串直接报错。
func writeDirResolve(script *strings.Builder, t shortcutTarget) {
	base := fmt.Sprintf("[Environment]::GetFolderPath('%s')", t.folder)
	if t.sub == "" {
		fmt.Fprintf(script, "$dir = %s\n", base)
		return
	}
	fmt.Fprintf(script, "$base = %s\n", base)
	fmt.Fprintf(script, "if ($base) { $dir = Join-Path $base '%s' } else { $dir = '' }\n", t.sub)
}
