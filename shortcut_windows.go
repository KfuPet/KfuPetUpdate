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

// shortcutName 是快捷方式的文件名（不含 .lnk）。
const shortcutName = "KfuPet"

// shortcutFolders 是创建/清理快捷方式时涉及的已知文件夹。
var shortcutFolders = []string{"Desktop", "Programs"}

// runPowerShell 以隐藏窗口的方式执行一段 PowerShell 脚本。
// 数据经环境变量传入，避免拼接脚本时出现引号转义问题。
func runPowerShell(script string, extraEnv []string) error {
	cmd := exec.Command("powershell",
		"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-Command", script)
	cmd.Env = append(os.Environ(), extraEnv...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}

	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%v（%s）", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// createShortcuts 按选项创建桌面与开始菜单快捷方式。
// 借系统自带的 WScript.Shell 写 .lnk，避免自行实现 .lnk 的二进制格式。
func createShortcuts(installDir string, opts installOptions) error {
	var folders []string
	if opts.Desktop {
		folders = append(folders, "Desktop")
	}
	if opts.StartMenu {
		folders = append(folders, "Programs")
	}
	if len(folders) == 0 {
		return nil
	}

	var script strings.Builder
	script.WriteString("$ErrorActionPreference = 'Stop'\n")
	script.WriteString("$shell = New-Object -ComObject WScript.Shell\n")
	for _, folder := range folders {
		// 用 GetFolderPath 解析，兼容桌面被重定向到别的盘（如 OneDrive）等情况。
		fmt.Fprintf(&script, "$dir = [Environment]::GetFolderPath('%s')\n", folder)
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

// removeShortcuts 删除安装时可能创建过的桌面与开始菜单快捷方式。
// 快捷方式本就不存在不算错误。
func removeShortcuts() error {
	var script strings.Builder
	script.WriteString("$ErrorActionPreference = 'Stop'\n")
	for _, folder := range shortcutFolders {
		fmt.Fprintf(&script, "$dir = [Environment]::GetFolderPath('%s')\n", folder)
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
