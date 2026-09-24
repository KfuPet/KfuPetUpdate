//go:build windows

package main

import (
	"errors"
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
	label  string // 给用户看的名字（修复报告里列出来）
}

// 安装时使用的公共落点：所有用户可见。
var (
	commonDesktop   = shortcutTarget{folder: "CommonDesktopDirectory", label: "桌面快捷方式"}
	commonStartMenu = shortcutTarget{folder: "CommonStartMenu", sub: "Programs", label: "开始菜单快捷方式"}
)

// installShortcutTargets 是安装（及修复）负责维护的落点，顺序固定。
var installShortcutTargets = []shortcutTarget{commonDesktop, commonStartMenu}

// allShortcutTargets 覆盖新旧两代落点：卸载时都要清。
// 当前用户那两处是早期版本建的，留着会变成指向已删程序的死链接。
var allShortcutTargets = []shortcutTarget{
	commonDesktop, commonStartMenu,
	{folder: "Desktop"}, {folder: "Programs"},
}

// runPowerShell 以隐藏窗口的方式执行一段 PowerShell 脚本。
// 数据经环境变量传入，避免拼接脚本时出现引号转义问题。
func runPowerShell(script string, extraEnv []string) error {
	_, err := runPowerShellOutput(script, extraEnv)
	return err
}

// runPowerShellOutput 执行脚本并返回标准输出，供调用方解析结果。
// 失败时把 stderr（PowerShell 的报错文本）带进错误信息，
// 否则调用方只能看到一个退出码，排不出是脚本哪一句出的问题。
func runPowerShellOutput(script string, extraEnv []string) (string, error) {
	cmd := exec.Command("powershell",
		"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-Command", script)
	cmd.Env = append(os.Environ(), extraEnv...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: winapi.CreateNoWindow}

	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		msg := ""
		if errors.As(err, &exitErr) {
			msg = strings.TrimSpace(string(exitErr.Stderr))
		}
		if msg == "" {
			return "", err
		}
		return "", fmt.Errorf("%v（%s）", err, msg)
	}
	return string(out), nil
}

// createShortcuts 按选项创建桌面与开始菜单快捷方式。
func createShortcuts(installDir string, opts installOptions) error {
	var targets []shortcutTarget
	if opts.Desktop {
		targets = append(targets, commonDesktop)
	}
	if opts.StartMenu {
		targets = append(targets, commonStartMenu)
	}
	return createShortcutsAt(installDir, targets)
}

// createShortcutsAt 在指定落点上创建（或覆盖）KfuPet 快捷方式。
// 借系统自带的 WScript.Shell 写 .lnk，避免自行实现 .lnk 的二进制格式。
func createShortcutsAt(installDir string, targets []shortcutTarget) error {
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

// shortcutState 是某个落点上快捷方式的现状。
type shortcutState int

const (
	shortcutMissing     shortcutState = iota // 不存在
	shortcutWrongTarget                      // 存在，但目标不是本安装目录的 KfuPet.exe
	shortcutOK                               // 存在且指向正确
)

// shortcutStates 检查各安装落点上的快捷方式现状，按 folder 名字返回。
// 落点目录取不到（如系统未安装该 shell 文件夹）时按"缺失"上报，由调用方决定是否重建。
func shortcutStates(installDir string) (map[string]shortcutState, error) {
	var script strings.Builder
	script.WriteString("$ErrorActionPreference = 'Stop'\n")
	script.WriteString("$shell = New-Object -ComObject WScript.Shell\n")
	for _, t := range installShortcutTargets {
		writeDirResolve(&script, t)
		// $dir 可能为空（取不到该文件夹），此时保持 missing，不当成错误。
		script.WriteString("$state = 'missing'\n")
		script.WriteString("$lnk = ''\n")
		fmt.Fprintf(&script, "if ($dir) { $lnk = Join-Path $dir '%s.lnk' }\n", shortcutName)
		script.WriteString("if ($lnk -and (Test-Path -LiteralPath $lnk)) {\n")
		script.WriteString("  if ($shell.CreateShortcut($lnk).TargetPath -eq $env:KFUPET_TARGET) { $state = 'ok' } else { $state = 'wrong' }\n")
		script.WriteString("}\n")
		fmt.Fprintf(&script, "Write-Output ('%s=' + $state)\n", t.folder)
	}

	out, err := runPowerShellOutput(script.String(), []string{
		"KFUPET_TARGET=" + filepath.Join(installDir, executableName),
	})
	if err != nil {
		return nil, err
	}

	states := make(map[string]shortcutState, len(installShortcutTargets))
	for _, line := range strings.Split(out, "\n") {
		name, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch value {
		case "ok":
			states[name] = shortcutOK
		case "wrong":
			states[name] = shortcutWrongTarget
		default:
			states[name] = shortcutMissing
		}
	}
	return states, nil
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
