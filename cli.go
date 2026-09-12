package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// action 是命令行要求 updater 执行的动作。
// 不带参数时为空，走正常的图形界面。
type action string

const (
	actionUpdate    action = "update"    // 升级：仅预留入口，尚未实现
	actionUninstall action = "uninstall" // 卸载：默认弹确认框，带 --yes 时静默执行
)

// command 是解析后的命令行参数。
type command struct {
	Action    action // 要执行的动作；空表示打开图形界面
	Dir       string // 目标安装目录；空表示读注册表记录
	Yes       bool   // 跳过确认（仅卸载有意义）
	PurgeData bool   // 卸载时一并删除个人数据
	Notify    bool   // 完成后弹窗告知结果（界面已退出、用户看不到进度的场景）
	WaitPIDs  []int  // 动手前需要等待退出的进程
}

// parseArgs 解析命令行。无法识别的参数一律忽略，保证多传参数不会让程序起不来。
func parseArgs(args []string) command {
	var c command
	for _, a := range args {
		switch {
		case a == "--yes":
			c.Yes = true
		case a == "--purge-data":
			c.PurgeData = true
		case a == "--notify":
			c.Notify = true
		case strings.HasPrefix(a, "--action="):
			c.Action = action(strings.TrimSpace(strings.TrimPrefix(a, "--action=")))
		case strings.HasPrefix(a, "--dir="):
			c.Dir = strings.TrimSpace(strings.TrimPrefix(a, "--dir="))
		case strings.HasPrefix(a, "--wait-pid="):
			c.WaitPIDs = append(c.WaitPIDs, parsePIDs(strings.TrimPrefix(a, "--wait-pid="))...)
		}
	}
	return c
}

// parsePIDs 解析逗号分隔的进程号，忽略非法项。
func parsePIDs(s string) []int {
	var pids []int
	for _, part := range strings.Split(s, ",") {
		if pid, err := strconv.Atoi(strings.TrimSpace(part)); err == nil && pid > 0 {
			pids = append(pids, pid)
		}
	}
	return pids
}

// args 把命令还原成命令行参数，供临时副本接力时原样交给下一个进程。
func (c command) args() []string {
	var args []string
	if c.Action != "" {
		args = append(args, "--action="+string(c.Action))
	}
	if c.Dir != "" {
		args = append(args, "--dir="+c.Dir)
	}
	if c.Yes {
		args = append(args, "--yes")
	}
	if c.PurgeData {
		args = append(args, "--purge-data")
	}
	if c.Notify {
		args = append(args, "--notify")
	}
	for _, pid := range c.WaitPIDs {
		args = append(args, fmt.Sprintf("--wait-pid=%d", pid))
	}
	return args
}

// modifiesInstallDir 表示该动作会删除或替换整个安装目录。
// 自身若正运行于该目录内，必须先交棒给临时副本，否则会因映像被占用而失败。
func (c command) modifiesInstallDir() bool {
	return c.Action == actionUninstall && c.Yes
}

// resolveInstallDir 返回本次操作的目标安装目录：优先命令行指定，其次注册表记录。
func resolveInstallDir(c command) string {
	if c.Dir != "" {
		return c.Dir
	}
	if rec, err := readInstallRecord(); err == nil && rec != nil {
		return rec.InstallPath
	}
	return ""
}

// runSilentUninstall 静默卸载：不带界面，直接删程序文件、快捷方式与安装信息。
// 默认保留个人数据，只有显式传 --purge-data 才一并删除。
func runSilentUninstall(c command) error {
	installDir := resolveInstallDir(c)
	if installDir == "" {
		return errors.New("未找到 KfuPet 的安装位置，无法卸载")
	}

	// 交棒过来的场景要等旧进程退出，否则安装目录里的文件仍被占用。
	waitForProcesses(c.WaitPIDs)

	if err := uninstallKfuPet(installDir, uninstallOptions{KeepUserData: !c.PurgeData}); err != nil {
		return err
	}
	if c.Notify {
		notifyInfo("KfuPet 卸载完成", "KfuPet 已卸载。")
	}
	return nil
}
