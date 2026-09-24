package main

import (
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"kfupet-installer/internal/uifx"
)

// newRepairingView 组装修复进行中页面：与安装中页面同构，只是标题与步骤清单不同。
// 步骤清单按本次是否需要下载安装包决定（见 repairStagesFor）。
func newRepairingView(s appState) *installingView {
	return newInstallingView("KfuPet 修复中", repairStagesFor(s.repair.needsDownload()), s)
}

// buildRepairCheckingView 组装体检中的全屏页。
// 体检要查注册表、探测快捷方式，必要时还要联网取哈希清单，耗时不定，
// 因此给个转圈 + 文案，而不是像其它流程那样先摆一张静态页。
func buildRepairCheckingView() fyne.CanvasObject {
	logoImage := canvas.NewImageFromResource(fyne.NewStaticResource("Startlogo.png", startLogoPNG))
	logoImage.FillMode = canvas.ImageFillContain
	logoBox := container.NewCenter(container.NewGridWrap(fyne.NewSize(96, 96), logoImage))

	spinner := uifx.NewSpinner()
	label := widget.NewLabelWithStyle("正在检查安装完整性", fyne.TextAlignCenter, fyne.TextStyle{})

	return container.NewCenter(container.NewVBox(
		logoBox,
		container.NewCenter(container.NewGridWrap(fyne.NewSize(40, 40), spinner)),
		container.NewCenter(container.NewHBox(label, uifx.NewDots())),
		container.NewCenter(smallText("KfuPetInstall")),
	))
}

// confirmRepair 展示体检报告，让用户确认是否开始修复。
// 报告要写清"要修什么、版本会不会变、要不要下载"——修复按线上最新版进行，
// 用户以为只是补个文件却被换了版本，是这里最容易让人意外的地方。
func confirmRepair(w fyne.Window, p repairPlan, onChoice func(start bool)) {
	lines := []string{"检查发现以下问题："}
	for _, line := range p.summary() {
		lines = append(lines, "· "+line)
	}
	if size := p.downloadSize(); size > 0 {
		lines = append(lines, fmt.Sprintf("需要重新下载安装包（约 %s）。", formatBytes(size)))
	}
	if p.needsDownload() {
		lines = append(lines, "只覆盖有问题的文件，角色模型等个人内容不受影响。")
	}
	// 拿不到清单时如实说明"程序文件本次没能校验"，而不是假装一切正常。
	if p.coreSkipReason != "" {
		lines = append(lines, "另外："+p.coreSkipReason+"，程序文件本次无法校验。")
	}
	if len(p.coreMissing) > 0 {
		lines = append(lines, "程序文件缺失："+strings.Join(p.coreMissing, "、")+"，需要联网才能补回。")
	}

	note := widget.NewLabel(strings.Join(lines, "\n"))
	note.Wrapping = fyne.TextWrapWord
	dialog.ShowCustomConfirm("修复 KfuPet", "开始修复", "取消", note, onChoice, w)
}

// showNothingToRepair 体检没发现问题时告知用户。
func showNothingToRepair(w fyne.Window, p repairPlan) {
	text := "安装目录与安装信息都完整，无需修复。"
	if v := versionText(p.version); v != "未知" {
		text += "当前版本 " + v + "。"
	}
	dialog.ShowInformation("无需修复", text, w)
}

// showRepairIncomplete 本地项都正常、但程序文件没能校验（无网等）时告知用户。
// 与"无需修复"分开说：后者是真的没问题，这里只是没查成。
func showRepairIncomplete(w fyne.Window, p repairPlan) {
	lines := []string{}
	if p.coreSkipReason != "" {
		lines = append(lines, p.coreSkipReason+"，程序文件本次无法校验。")
	}
	if len(p.coreMissing) > 0 {
		lines = append(lines, "程序文件缺失："+strings.Join(p.coreMissing, "、")+"。")
	}
	lines = append(lines, "本地安装信息已检查过，没有问题。请检查网络后重新修复。")
	dialog.ShowInformation("修复未完成", strings.Join(lines, "\n"), w)
}

// buildRepairDoneView 展示修复结果：列出实际补回的东西，再问是否启动。
func buildRepairDoneView(installDir, version string, done []string, onLaunch, onQuit func()) fyne.CanvasObject {
	title := canvas.NewText("修复完成", theme.Color(theme.ColorNameForeground))
	title.TextSize = 24
	title.TextStyle = fyne.TextStyle{Bold: true}

	verLine := " "
	if version != "" {
		verLine = "KfuPet " + version
	}

	items := []fyne.CanvasObject{
		layout.NewSpacer(),
		container.NewCenter(container.NewGridWrap(fyne.NewSize(72, 72), uifx.NewResultMark(true))),
		container.NewCenter(title),
		container.NewCenter(smallText(verLine)),
	}
	if len(done) > 0 {
		items = append(items, container.NewCenter(smallText("已处理：")))
		for _, line := range done {
			items = append(items, container.NewCenter(smallText("· "+line)))
		}
	}
	items = append(items,
		container.NewCenter(smallText("安装位置："+installDir)),
		container.NewCenter(widget.NewLabelWithStyle("是否立即启动 KfuPet？", fyne.TextAlignCenter, fyne.TextStyle{})),
		container.NewCenter(container.NewHBox(
			actionButton("是", onLaunch),
			actionButton("否", onQuit),
		)),
		layout.NewSpacer(),
	)

	// 彩带层盖在内容之上，修复成功时炸开一次庆祝。
	return container.NewStack(container.NewVBox(items...), uifx.NewConfetti())
}
