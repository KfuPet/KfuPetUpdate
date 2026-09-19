package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"kfupet-installer/internal/version"
	"kfupet-installer/internal/winapi"
	"kfupet-installer/internal/winreg"
)

// upgradeWaitTimeout 是拉起本程序后等待桌宠退出的窗口。
// 桌宠收到「立即更新」会立刻自行退出，正常毫秒级就让位了，这个窗口只是覆盖它
// 保存状态、释放资源的那点时间。等待本身可打断：进程一退出立即继续，不干等满。
const upgradeWaitTimeout = 4 * time.Second

// upgradeKillWaitTimeout 是强杀桌宠后，等待其映像释放的最长时间。
const upgradeKillWaitTimeout = 5 * time.Second

// 升级流程特有的两个阶段；其后的阶段与安装完全一致（见 upgradeStages）。
const (
	stageWaitingKfuPet   installStage = "正在等待 KfuPet 退出"
	stageCheckingVersion installStage = "正在检查新版本"
)

// killConfirmFunc 在等待进程退出超时后询问用户是否强行终止，返回 true 表示同意。
// 用 ctx 是为了让用户迟迟不回答时也能随整体流程超时退出，不至于卡死。
type killConfirmFunc func(ctx context.Context, pid int) bool

// upgradeCheck 是升级前的版本比较结论。
type upgradeCheck struct {
	local  string // 本地已安装版本（注册表记录，可能为空）
	latest string // 远端最新版本（已归一）
	cmp    int    // version.Compare(local, latest)：<0 远端更新，=0 相同，>0 本地更新
}

// NeedUpgrade 表示远端版本高于本地，需要升级。
func (c upgradeCheck) NeedUpgrade() bool { return c.cmp < 0 }

// RemoteOlder 表示远端版本低于本地（本机比线上还新）。
// 这种情况不能提示「已经是最新版本」——那是错的，得单独说明。
func (c upgradeCheck) RemoteOlder() bool { return c.cmp > 0 }

// upgradeOutcome 是一次升级流程的结论。
type upgradeOutcome struct {
	check  upgradeCheck  // 版本比较结论
	result installResult // 实际安装的产物（check.NeedUpgrade() 为假时无意义）
}

// upgradeInstallOptions 推导升级时使用的安装选项：不问用户，按本机现状决定。
// 安装目录不变，安装时创建的快捷方式仍指向同一个路径，因此无需重建；
// 也不该凭空补上用户当初没勾选的快捷方式。运行环境同理：KfuPet 能跑起来就
// 说明运行环境已经装好了，不重复安装。
func upgradeInstallOptions() installOptions {
	return installOptions{}
}

// upgradeStages 返回升级流程的步骤清单。
// 由桌宠拉起（要等它让位）时多一步等待；从主界面直接升级时本就没有别的进程在跑，
// 不需要这一步。其余步骤与安装共用，且升级不装运行环境、不建快捷方式，会自动跳过。
func upgradeStages(waitKfuPet bool) []installStage {
	stages := make([]installStage, 0, len(installStages)+2)
	if waitKfuPet {
		stages = append(stages, stageWaitingKfuPet)
	}
	stages = append(stages, stageCheckingVersion)
	return append(stages, stagesFor(upgradeInstallOptions())...)
}

// kfuPetRunning 判断安装目录内的 KfuPet 是否正在运行。
// 升级要整体替换安装目录，正在运行的 exe 映像替换不掉，因此动手前先拦住用户。
func kfuPetRunning(installDir string) bool {
	return isExecutableBusy(filepath.Join(installDir, executableName))
}

// localVersion 返回注册表记录的本地版本号；记录不存在或未记版本时返回空串。
func localVersion() string {
	rec, err := winreg.ReadInstallRecord()
	if err != nil || rec == nil {
		return ""
	}
	return rec.DisplayVersion
}

// compareUpgrade 比较本地与远端版本。
// 远端可能带 v 前缀、段数也可能与本地不同，全交给 version 包归一后按段比较。
func compareUpgrade(local, remote string) upgradeCheck {
	local, latest := version.Normalize(local), version.Normalize(remote)
	return upgradeCheck{
		local:  local,
		latest: latest,
		cmp:    version.Compare(local, latest),
	}
}

// upgradeKfuPet 执行一次升级：等桌宠让位 → 查最新版本 → 版本比较 → 需要时整体安装。
// 远端不高于本地时不做任何改动，由调用方按 outcome.check 给出对应提示。
func upgradeKfuPet(ctx context.Context, checker *updateChecker, installDir string, waitPIDs []int, confirmKill killConfirmFunc, report progressFunc) (upgradeOutcome, error) {
	var out upgradeOutcome
	if strings.TrimSpace(installDir) == "" {
		return out, errors.New("未找到 KfuPet 的安装位置，无法升级")
	}

	// 桌宠会先自行退出再让我们动手，这里等它真正让位。
	// 没有 --wait-pid（从主界面点升级）时无需等待，占用检测由安装流程兜底。
	if len(waitPIDs) > 0 {
		reportStage(report, stageWaitingKfuPet)
		if err := waitTargetsExit(ctx, waitPIDs, confirmKill); err != nil {
			return out, err
		}
	}

	reportStage(report, stageCheckingVersion)
	rel, err := checker.check(ctx)
	if err != nil {
		return out, err
	}

	out.check = compareUpgrade(localVersion(), rel.Version)
	if !out.check.NeedUpgrade() {
		return out, nil
	}

	out.result, err = installKfuPet(ctx, rel, installDir, upgradeInstallOptions(), report)
	return out, err
}

// waitTargetsExit 等待需要让位的进程退出。
// 等待可打断：进程一退出就立即继续，不会干等满 upgradeWaitTimeout。
// 超时仍有进程在跑时交给 confirmKill 询问用户；用户不同意终止则放弃本次升级。
func waitTargetsExit(ctx context.Context, pids []int, confirmKill killConfirmFunc) error {
	for _, pid := range pids {
		if pid <= 0 || pid == os.Getpid() {
			continue
		}
		if winapi.WaitProcessExitTimeout(pid, upgradeWaitTimeout) {
			continue
		}

		if confirmKill == nil || !confirmKill(ctx, pid) {
			return fmt.Errorf("KfuPet（进程 %d）仍在运行，已放弃升级", pid)
		}
		if err := winapi.KillProcess(pid); err != nil {
			return fmt.Errorf("终止 KfuPet 进程失败：%w", err)
		}
		// 强杀后映像释放还要一点时间，等它退出再继续替换目录。
		winapi.WaitProcessExitTimeout(pid, upgradeKillWaitTimeout)
	}
	return nil
}
