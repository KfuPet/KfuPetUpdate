package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"kfupet-installer/internal/uifx"
)

// upgradeRelaunchDelay 是升级完成后停留多久再自动拉起 KfuPet。
// 留一点时间让完成页的彩带露个面，随后自动启动，用户几乎无感。
const upgradeRelaunchDelay = 1200 * time.Millisecond

// newUpgradingView 组装升级进行中页面：与安装中页面同构，只是标题与步骤清单不同。
// 步骤清单按是否需要等桌宠让位决定（见 upgradeStages）。
func newUpgradingView(s appState) *installingView {
	return newInstallingView("KfuPet 升级中", upgradeStages(s.waitKfuPet), s)
}

// buildUpgradeDoneView 展示升级完成：描边对勾 + 彩带庆祝，随后自动拉起新版 KfuPet。
// 只用于桌宠拉起的那条路径——它是被拉起来的、没人守着界面，升完就该把桌宠还回去；
// 手动升级走 buildDoneView，会先问一句是否启动。
func buildUpgradeDoneView(localVersion, installDir string) fyne.CanvasObject {
	title := canvas.NewText("升级完成", theme.Color(theme.ColorNameForeground))
	title.TextSize = 24
	title.TextStyle = fyne.TextStyle{Bold: true}

	versionText := " "
	if localVersion != "" {
		versionText = "KfuPet " + localVersion
	}

	hint := widget.NewLabel("正在启动 KfuPet，请稍候…")
	hint.Alignment = fyne.TextAlignCenter

	content := container.NewVBox(
		layout.NewSpacer(),
		container.NewCenter(container.NewGridWrap(fyne.NewSize(72, 72), uifx.NewResultMark(true))),
		container.NewCenter(title),
		container.NewCenter(smallText(versionText)),
		container.NewCenter(smallText("安装位置："+installDir)),
		container.NewCenter(hint),
		layout.NewSpacer(),
	)
	// 彩带层盖在内容之上，升级成功时炸开一次庆祝。
	return container.NewStack(content, uifx.NewConfetti())
}

// showUpgradeSkipped 提示「没有可升级的版本」。
// 远端低于本地说明本机比线上还新，这时写成「已经是最新版本」是错的，得分两种提示。
func showUpgradeSkipped(w fyne.Window, c upgradeCheck) {
	if c.RemoteOlder() {
		dialog.ShowInformation("无需升级",
			fmt.Sprintf("本机版本 v%s 比线上版本 v%s 更新，无需升级。",
				versionLabel(c.local), versionLabel(c.latest)), w)
		return
	}
	dialog.ShowInformation("已是最新版本",
		fmt.Sprintf("当前版本 v%s 已经是最新版本。", versionLabel(c.local)), w)
}

// versionLabel 生成版本号展示文案；空值兜底为「未知」，避免出现「v」这样的残缺提示。
func versionLabel(v string) string {
	if strings.TrimSpace(v) == "" {
		return "未知"
	}
	return v
}

// killConfirm 返回「等待桌宠退出超时后是否强杀」的询问回调。
// 弹窗必须在主线程执行，而询问发生在流程 goroutine 里，因此用 channel 把答案带回去；
// 用户迟迟不回答时随 ctx 结束返回 false（放弃升级），不会一直卡死。
func killConfirm(w fyne.Window) killConfirmFunc {
	return func(ctx context.Context, pid int) bool {
		answer := make(chan bool, 1)
		fyne.Do(func() {
			note := widget.NewLabel(fmt.Sprintf(
				"等待 KfuPet（进程 %d）退出超时，无法替换安装目录。\n是否强行终止它？未保存的内容可能丢失。", pid))
			note.Wrapping = fyne.TextWrapWord
			dialog.ShowCustomConfirm("KfuPet 未退出", "强行终止", "取消", note,
				func(ok bool) { answer <- ok }, w)
		})

		select {
		case ok := <-answer:
			return ok
		case <-ctx.Done():
			return false
		}
	}
}

// buildUpdateReadyView 组装「等待用户确认更新」的页面，作为确认弹窗的背景。
// 用户点「继续更新」后立刻切到升级进度页，所以这里只是个静态的准备态。
func buildUpdateReadyView() fyne.CanvasObject {
	logoImage := canvas.NewImageFromResource(fyne.NewStaticResource("Startlogo.png", startLogoPNG))
	logoImage.FillMode = canvas.ImageFillContain
	logoBox := container.NewCenter(container.NewGridWrap(fyne.NewSize(96, 96), logoImage))

	title := widget.NewLabelWithStyle("准备更新 KfuPet", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})

	return container.NewCenter(container.NewVBox(
		logoBox,
		container.NewCenter(title),
		container.NewCenter(smallText("请在弹出的提示框中选择是否继续。")),
	))
}

// confirmUpgrade 被桌宠拉起后先问用户一句再动手。
// 升级会重新下载安装包并整体替换安装目录，不该在用户没点头的情况下就开始。
func confirmUpgrade(w fyne.Window, onChoice func(continueUpdate bool)) {
	note := widget.NewLabel("更新会替换 KfuPet 的程序文件，过程中 KfuPet 会暂时关闭，完成后自动重新启动。")
	note.Wrapping = fyne.TextWrapWord
	dialog.ShowCustomConfirm("是否继续更新？", "继续更新", "暂不更新", note, onChoice, w)
}

// scheduleRelaunch 稍作停留后自动拉起升级好的 KfuPet，并退出 updater。
// 拉起失败就留在完成页并报错——此时升级本身已经成功，用户手动关掉本程序再启动即可。
func scheduleRelaunch(a fyne.App, w fyne.Window, installDir string) {
	time.AfterFunc(upgradeRelaunchDelay, func() {
		// 定时器在独立 goroutine 上触发，操作界面必须回到主线程。
		fyne.Do(func() {
			if err := launchKfuPet(installDir); err != nil {
				dialog.ShowError(err, w)
				return
			}
			a.Quit()
		})
	})
}
