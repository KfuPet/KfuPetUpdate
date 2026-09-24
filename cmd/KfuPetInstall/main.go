package main

import (
	"context"
	_ "embed"
	"fmt"
	"image/color"
	_ "image/png"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"kfupet-installer/internal/dotnet"
	"kfupet-installer/internal/uifx"
	"kfupet-installer/internal/winapi"
)

//go:embed icon/Startlogo.png
var startLogoPNG []byte

//go:generate windres -c 65001 app.rc -O coff -o app_windows_amd64.syso

type vfillLayout struct {
	width      float32 // 左侧栏整体宽度
	itemHeight float32 // 每个按钮的高度
	spacing    float32 // 按钮之间的间距
}

func (l *vfillLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	n := float32(len(objects))
	total := n*l.itemHeight + (n-1)*l.spacing
	bottomMargin := float32(16)
	y := size.Height - total - bottomMargin
	leftMargin := float32(16)
	for _, o := range objects {
		o.Resize(fyne.NewSize(size.Width-leftMargin, l.itemHeight))
		o.Move(fyne.NewPos(leftMargin, y))
		y += l.itemHeight + l.spacing
	}
}

func (l *vfillLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	n := float32(len(objects))
	h := n*l.itemHeight + (n-1)*l.spacing
	return fyne.NewSize(l.width, h)
}

type linkText struct {
	widget.BaseWidget
	text *canvas.Text
	url  *url.URL
}

func newLinkText(label string, u *url.URL) *linkText {
	t := canvas.NewText(label, color.Gray{Y: 0x99})
	t.TextStyle = fyne.TextStyle{Underline: true}
	t.TextSize = theme.CaptionTextSize()
	return &linkText{text: t, url: u}
}

func (l *linkText) CreateRenderer() fyne.WidgetRenderer {
	l.ExtendBaseWidget(l)
	return &linkTextRenderer{linkText: l}
}

func (l *linkText) Tapped(*fyne.PointEvent) {
	if l.url != nil {
		_ = fyne.CurrentApp().OpenURL(l.url)
	}
}

type linkTextRenderer struct {
	linkText *linkText
}

func (r *linkTextRenderer) Layout(size fyne.Size) {
	r.linkText.text.Resize(size)
}

func (r *linkTextRenderer) MinSize() fyne.Size {
	return r.linkText.text.MinSize()
}

func (r *linkTextRenderer) Refresh() {
	r.linkText.text.Refresh()
}

func (r *linkTextRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.linkText.text}
}

func (r *linkTextRenderer) Destroy() {}

func smallText(s string) *canvas.Text {
	t := canvas.NewText(s, color.Gray{Y: 0x99})
	t.TextSize = theme.CaptionTextSize()
	return t
}

// warnLabel 展示被降级为警告的问题（如快捷方式重试后仍未建出来）：
// 主题警示色 + 可换行，与正文区分得开，又不像错误页那样中断流程。
func warnLabel(s string) *widget.Label {
	l := widget.NewLabel("· " + s)
	l.Importance = widget.WarningImportance
	l.Wrapping = fyne.TextWrapWord
	l.Alignment = fyne.TextAlignCenter
	return l
}

// actionButtonSize 是各页面操作按钮的统一尺寸。
// 按钮宽度默认只由文字决定，单字按钮（如"是""否"）会窄得不好点，故统一给定尺寸。
var actionButtonSize = fyne.NewSize(100, 40)

// actionButton 生成统一尺寸的操作按钮。
func actionButton(label string, tapped func()) fyne.CanvasObject {
	return container.NewGridWrap(actionButtonSize, widget.NewButton(label, tapped))
}

func bottomBar() fyne.CanvasObject {
	kfupetURL, _ := url.Parse("https://github.com/KfuPet")
	copyrightText := canvas.NewText("Copyright © 2025 - 2026 ", color.Gray{Y: 0x99})
	copyrightText.TextSize = theme.CaptionTextSize()
	copyrightLine := container.NewHBox(copyrightText, newLinkText("KfuPet", kfupetURL))
	return container.NewBorder(
		widget.NewSeparator(),              // 顶部细横线，横贯窗口宽度
		nil,                                // 底部
		nil,                                // 左侧
		nil,                                // 右侧
		container.NewCenter(copyrightLine), // 版权信息居中
	)
}

// buildVersionPanel 展示远端最新版本信息；已安装时补一行本地版本。
func buildVersionPanel(rel *releaseInfo, st installState) fyne.CanvasObject {
	caption := smallText("KfuPet 最新版本")
	caption.TextStyle = fyne.TextStyle{}

	versionText := canvas.NewText(rel.Version, theme.Color(theme.ColorNameForeground))
	versionText.TextSize = 32
	versionText.TextStyle = fyne.TextStyle{Bold: true}

	rows := []fyne.CanvasObject{
		container.NewCenter(caption),
		container.NewCenter(versionText),
		container.NewCenter(smallText(publishLine(rel))),
	}
	if rel.ReleasePageURL != "" {
		if u, err := url.Parse(rel.ReleasePageURL); err == nil {
			rows = append(rows, container.NewCenter(newLinkText("前往 GitHub 查看发布说明", u)))
		}
	}
	// 已安装时补一行本地版本（取自注册表），便于确认升级方向。
	if st.Installed && st.Version != "" {
		rows = append(rows, container.NewCenter(smallText("当前已安装 "+st.Version)))
	}
	return container.NewCenter(container.NewVBox(rows...))
}

// publishLine 生成"发布于 yyyy-MM-dd"文本；时间为零值时提示未知。
func publishLine(rel *releaseInfo) string {
	if rel.PublishedAt.IsZero() {
		return "发布日期未知"
	}
	return "发布于 " + rel.PublishedAt.Local().Format("2006-01-02")
}

// installPhase 表示界面当前处于安装流程的哪个位置。
type installPhase int

const (
	phaseIdle           installPhase = iota // 不在流程中，展示主界面
	phaseChooseDir                          // 向导第一步：选择安装位置
	phaseOptions                            // 向导第二步：安装选项
	phaseRunning                            // 正在安装
	phaseDone                               // 安装完成
	phaseFailed                             // 安装失败
	phaseUpgrading                          // 正在升级
	phaseUpgradeDone                        // 升级完成
	phaseUpgradeFailed                      // 升级失败
	phaseRepairChecking                     // 修复第一步：体检
	phaseRepairing                          // 正在修复
	phaseRepairDone                         // 修复完成
	phaseRepairFailed                       // 修复失败
)

// appState 是界面的全部可变状态；每次变化后整体重建界面。
type appState struct {
	rel      *releaseInfo // 远端发布信息；查询失败时为 nil
	checkErr error        // 版本查询失败原因
	st       installState // 本机安装状态
	env      dotnet.State // 本机运行环境（.NET 桌面运行时）检测结果

	phase      installPhase
	targetDir  string          // 本次安装的目标目录
	opts       installOptions  // 本次安装的选项
	progress   installProgress // 最近一次进度汇报
	installErr error           // 安装/升级/修复失败原因
	waitKfuPet bool            // 升级是否由桌宠拉起：决定升级步骤清单是否含「等待退出」
	repair     repairPlan      // 本次修复的体检结论与执行清单
	warnings   []string        // 本次流程中被降级为警告的问题，结果页据此多提示一句
}

// uiHandlers 是主界面各操作入口。
// 已安装状态下没有「升级」：升级要先把 KfuPet 关掉，而只有 KfuPet 自己知道
// 什么时候能退，因此升级只由它发起（`--action=update`）；手动入口提供的是「修复」——
// 补齐被杀软清掉或被误删的文件与安装信息（见 README「修复流程」）。
type uiHandlers struct {
	retryCheck func()
	install    func()
	repair     func()
	uninstall  func()
}

// flowHandlers 是安装向导各页面的操作入口。
type flowHandlers struct {
	browse       func()     // 第一步：自定义安装位置
	next         func()     // 第一步 → 第二步
	back         func()     // 第二步 → 第一步
	install      func()     // 第二步 → 开始安装
	retry        func()     // 安装失败后按当前选项重试
	backToMain   func()     // 安装失败后返回主界面
	launch       func()     // 安装完成后启动 KfuPet 并退出 updater
	quit         func()     // 安装完成后不启动，直接退出 updater
	setDesktop   func(bool) // 勾选/取消桌面快捷方式
	setStartMenu func(bool) // 勾选/取消开始菜单快捷方式
	setOffline   func(bool) // 切换在线/离线安装
	pickPackage  func()     // 离线安装：选择本地安装包
}

// buildMainUI 组装主界面。
// 可用操作由注册表安装状态决定：未安装只提供「安装」，
// 已安装提供「修复」「卸载」，「安装」不再出现。
// 返回内容连同左上角 Logo 的遮罩，供闪屏 Logo 渐隐后衔接渐显。
func buildMainUI(s appState, h uiHandlers) (fyne.CanvasObject, *uifx.Cover) {
	var actions []fyne.CanvasObject
	if s.st.Installed {
		actions = append(actions,
			widget.NewButton("修复", h.repair),
			widget.NewButton("卸载", h.uninstall),
		)
	} else {
		actions = append(actions, widget.NewButton("安装", h.install))
	}

	// 上方图标：从资源嵌入的 KfuPet Logo；外包一层遮罩，用于进入主界面时渐显。
	logoImage := canvas.NewImageFromResource(fyne.NewStaticResource("Startlogo.png", startLogoPNG))
	logoImage.FillMode = canvas.ImageFillContain
	logoArea, logoCover := uifx.NewCover(logoImage)
	logoBox := container.NewCenter(container.NewGridWrap(fyne.NewSize(120, 120), logoArea))

	// 左侧：上方图标 + 下方按钮（数量随安装状态变化）
	buttons := container.New(&vfillLayout{
		width:      180,
		itemHeight: 48,
		spacing:    20,
	}, actions...)
	left := container.NewBorder(logoBox, nil, nil, nil, buttons)

	return container.NewBorder(nil, bottomBar(), left, nil, buildRightPanel(s, h)), logoCover
}

// buildRightPanel 组装主界面右侧信息面板。
func buildRightPanel(s appState, h uiHandlers) fyne.CanvasObject {
	if s.checkErr != nil {
		return buildFailedPanel("无法获取线上版本", s.checkErr, "重试", h.retryCheck)
	}
	return buildVersionPanel(s.rel, s.st)
}

// buildInstallView 组装安装向导的全屏页面。
// 安装不再是主界面的一部分，而是独立成页；结束后由用户返回主界面。
// 进行中的页面（phaseRunning）不经过这里：它由 render 直接维护，以便原地更新。
func buildInstallView(s appState, h flowHandlers) fyne.CanvasObject {
	switch s.phase {
	case phaseOptions:
		return buildOptionsPage(s, h)
	case phaseDone:
		return buildDoneView("安装完成", s.st.Version, s.targetDir, s.warnings, h.launch, h.quit)
	case phaseFailed:
		return buildInstallFailedView("安装失败", s.installErr, s.targetDir, h.retry, h.backToMain)
	default:
		return buildChooseDirPage(s, h)
	}
}

// buildChooseDirPage 向导第一步：确认或自定义安装位置。
func buildChooseDirPage(s appState, h flowHandlers) fyne.CanvasObject {
	caption := smallText("KfuPet 安装向导")
	caption.TextStyle = fyne.TextStyle{}

	title := canvas.NewText("选择安装位置", theme.Color(theme.ColorNameForeground))
	title.TextSize = 20
	title.TextStyle = fyne.TextStyle{Bold: true}

	pathLabel := widget.NewLabel(s.targetDir)
	pathLabel.Alignment = fyne.TextAlignCenter
	pathLabel.Wrapping = fyne.TextWrapWord

	return container.NewVBox(
		layout.NewSpacer(),
		container.NewCenter(caption),
		container.NewCenter(title),
		container.NewCenter(smallText("程序将安装到下面的目录：")),
		pathLabel,
		container.NewCenter(actionButton("浏览…", h.browse)),
		container.NewCenter(actionButton("下一步", h.next)),
		layout.NewSpacer(),
	)
}

// 安装方式在两个单选项间切换：在线安装从网络下载安装包，离线安装直接用本地已有的包。
const (
	modeOnline  = "在线安装"
	modeOffline = "离线安装"
)

// buildOptionsPage 向导第二步：选择安装方式与是否创建快捷方式。
func buildOptionsPage(s appState, h flowHandlers) fyne.CanvasObject {
	caption := smallText("KfuPet 安装向导")
	caption.TextStyle = fyne.TextStyle{}

	title := canvas.NewText("安装选项", theme.Color(theme.ColorNameForeground))
	title.TextSize = 20
	title.TextStyle = fyne.TextStyle{Bold: true}

	desktopCheck := widget.NewCheck("创建桌面快捷方式", h.setDesktop)
	desktopCheck.SetChecked(s.opts.Desktop)
	startMenuCheck := widget.NewCheck("创建开始菜单快捷方式", h.setStartMenu)
	startMenuCheck.SetChecked(s.opts.StartMenu)

	// 先设初值再挂回调，避免建控件时就触发一次切换。
	modeRadio := widget.NewRadioGroup([]string{modeOnline, modeOffline}, nil)
	modeRadio.Horizontal = true
	if s.opts.Offline {
		modeRadio.SetSelected(modeOffline)
	} else {
		modeRadio.SetSelected(modeOnline)
	}
	modeRadio.OnChanged = func(v string) { h.setOffline(v == modeOffline) }

	items := []fyne.CanvasObject{
		layout.NewSpacer(),
		container.NewCenter(caption),
		container.NewCenter(title),
		container.NewCenter(desktopCheck),
		container.NewCenter(startMenuCheck),
		container.NewCenter(modeRadio),
	}
	// 运行环境缺失时会在进向导前询问是否一并安装，这里把结果如实列出。
	if s.opts.InstallEnv {
		items = append(items, container.NewCenter(smallText("将一并安装运行环境："+dotnet.DisplayName)))
	}
	if s.opts.Offline {
		items = append(items,
			container.NewCenter(smallText("安装包："+packageLabel(s.opts.Package))),
			container.NewCenter(actionButton("选择安装包…", h.pickPackage)),
		)
	}
	// 主按钮文案随安装方式变化：在线安装 / 离线安装。
	installLabel := modeOnline
	if s.opts.Offline {
		installLabel = modeOffline
	}
	items = append(items,
		container.NewCenter(smallText("安装位置："+s.targetDir)),
		container.NewCenter(container.NewHBox(
			actionButton("上一步", h.back),
			actionButton(installLabel, h.install),
		)),
		layout.NewSpacer(),
	)

	return container.NewVBox(items...)
}

// packageLabel 返回离线安装包在界面上的展示文案。
func packageLabel(path string) string {
	if strings.TrimSpace(path) == "" {
		return "未选择"
	}
	return path
}

// installingView 是安装进行中的页面：创建一次后由进度回调原地更新，
// 步骤清单的画勾/脉冲、进度条流光等动画才不会被整页重建打断。
type installingView struct {
	root      fyne.CanvasObject
	stage     *canvas.Text
	stageBox  *fyne.Container // 文案变长变短后靠 Refresh 重新居中
	detail    *canvas.Text
	detailBox *fyne.Container
	steps     *uifx.StepList
	deter     *uifx.ShineBar              // 下载阶段：确定进度条 + 流光
	indet     *widget.ProgressBarInfinite // 其余阶段：不确定进度条
	stages    []installStage
}

// newInstallingView 组装进行中页面，并按当前进度初始化显示。
// 安装与升级共用这套页面，标题与步骤清单由调用方给出（两者步骤不同）。
func newInstallingView(title string, stages []installStage, s appState) *installingView {
	v := &installingView{stages: stages}

	caption := smallText(title)

	v.stage = canvas.NewText(" ", theme.Color(theme.ColorNameForeground))
	v.stage.TextSize = 20
	v.stage.TextStyle = fyne.TextStyle{Bold: true}

	// 下载阶段总量已知：用确定进度条并展示速度与字节数；
	// 其余阶段无法量化，用不确定进度条表示"正在进行"。两条进度条叠放，按需切换。
	v.deter = uifx.NewShineBar()
	v.indet = widget.NewProgressBarInfinite()
	bars := container.NewGridWrap(fyne.NewSize(360, 26), container.NewStack(v.deter, v.indet))

	v.detail = canvas.NewText(" ", color.Gray{Y: 0x99})
	v.detail.TextSize = theme.CaptionTextSize()
	v.detail.Alignment = fyne.TextAlignCenter

	v.steps = uifx.NewStepList(stageNames(v.stages))

	v.stageBox = container.NewCenter(v.stage)
	v.detailBox = container.NewCenter(v.detail)
	v.root = container.NewCenter(container.NewVBox(
		container.NewCenter(caption),
		v.stageBox,
		bars,
		v.detailBox,
		container.NewCenter(v.steps),
		container.NewCenter(smallText("安装位置："+s.targetDir)),
	))
	v.update(s.progress)
	return v
}

// update 按一次进度汇报原地刷新页面。
func (v *installingView) update(p installProgress) {
	v.stage.Text = string(p.Stage)
	v.stage.Refresh()
	v.stageBox.Refresh() // 阶段文案长度变化后重新居中
	v.steps.SetCurrent(stageIndex(v.stages, p.Stage))

	// 运行环境与安装包的下载都带字节进度，用确定进度条展示；
	// 其余阶段无法量化，用不确定进度条表示"正在进行"。
	if (p.Stage == stageDownloading || p.Stage == stageEnvDownloading) && p.Total > 0 {
		v.indet.Stop()
		v.indet.Hide()
		v.deter.Show()
		v.deter.SetMax(float64(p.Total))
		v.deter.SetValue(float64(p.Done))
		v.detail.Text = fmt.Sprintf("%s / %s　%s/s",
			formatBytes(p.Done), formatBytes(p.Total), formatBytes(int64(p.Speed)))
	} else {
		v.deter.Hide()
		v.indet.Show()
		v.indet.Start()
		v.detail.Text = " "
	}
	v.detail.Refresh()
	v.detailBox.Refresh()
}

// stageNames 把阶段枚举转成步骤清单的展示文案（带序号）。
func stageNames(stages []installStage) []string {
	names := make([]string, len(stages))
	for i, s := range stages {
		names[i] = fmt.Sprintf("%d. %s", i+1, s)
	}
	return names
}

// buildDoneView 展示安装/升级结果，并询问是否立即启动 KfuPet。
// 两个选项都会退出 updater：流程已经走完，没有留在界面上的必要。
// warnings 是本次被降级为警告的问题（如快捷方式没建成），列在版本号下方。
func buildDoneView(doneTitle, version, installDir string, warnings []string, onLaunch, onQuit func()) fyne.CanvasObject {
	title := canvas.NewText(doneTitle, theme.Color(theme.ColorNameForeground))
	title.TextSize = 24
	title.TextStyle = fyne.TextStyle{Bold: true}

	versionText := " "
	if version != "" {
		versionText = "KfuPet " + version
	}

	question := widget.NewLabel("是否立即启动 KfuPet？")
	question.Alignment = fyne.TextAlignCenter

	items := []fyne.CanvasObject{
		layout.NewSpacer(),
		container.NewCenter(container.NewGridWrap(fyne.NewSize(72, 72), uifx.NewResultMark(true))),
		container.NewCenter(title),
		container.NewCenter(smallText(versionText)),
	}
	for _, line := range warnings {
		items = append(items, container.NewCenter(warnLabel(line)))
	}
	items = append(items,
		container.NewCenter(smallText("安装位置："+installDir)),
		container.NewCenter(question),
		container.NewCenter(container.NewHBox(
			actionButton("是", onLaunch),
			actionButton("否", onQuit),
		)),
		layout.NewSpacer(),
	)

	// 彩带层盖在内容之上，流程成功时炸开一次庆祝。
	return container.NewStack(container.NewVBox(items...), uifx.NewConfetti())
}

// buildInstallFailedView 展示流程失败原因与重试、返回入口；安装与升级共用。
func buildInstallFailedView(failTitle string, failure error, installDir string, onRetry, onBack func()) fyne.CanvasObject {
	title := widget.NewLabelWithStyle(failTitle, fyne.TextAlignCenter, fyne.TextStyle{Bold: true})

	reason := "未知错误"
	if failure != nil {
		reason = failure.Error()
	}
	detail := widget.NewLabel("原因：" + reason)
	detail.Alignment = fyne.TextAlignCenter
	detail.Wrapping = fyne.TextWrapWord

	return container.NewVBox(
		layout.NewSpacer(),
		container.NewCenter(container.NewGridWrap(fyne.NewSize(72, 72), uifx.NewResultMark(false))),
		title,
		detail,
		container.NewCenter(smallText("安装位置："+installDir)),
		container.NewCenter(container.NewHBox(
			actionButton("重试", onRetry),
			actionButton("返回", onBack),
		)),
		layout.NewSpacer(),
	)
}

// pickInstallDir 让用户选择安装目录；取消或选择失败时不回调。
// startDir 用于定位对话框的初始位置。
func pickInstallDir(w fyne.Window, startDir string, onPicked func(string)) {
	d := dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
		if err != nil || uri == nil {
			return // 用户取消，或选择过程中出错：保持当前页面不变
		}
		// Fyne 返回的路径在 Windows 上是正斜杠形式，需转回本地分隔符。
		onPicked(filepath.Clean(filepath.FromSlash(uri.Path())))
	}, w)

	if start := pickerStartDir(startDir); start != "" {
		if loc, err := storage.ListerForURI(storage.NewFileURI(start)); err == nil {
			d.SetLocation(loc)
		}
	}
	d.Show()
}

// pickInstallPackage 让用户选择本地已有的离线安装包（zip）；取消或选择失败时不回调。
// current 为已选中的安装包路径，用于定位对话框的初始位置。
func pickInstallPackage(w fyne.Window, current string, onPicked func(string)) {
	d := dialog.NewFileOpen(func(rc fyne.URIReadCloser, err error) {
		if err != nil || rc == nil {
			return // 用户取消，或选择过程中出错：保持当前页面不变
		}
		uri := rc.URI()
		_ = rc.Close()
		// Fyne 返回的路径在 Windows 上是正斜杠形式，需转回本地分隔符。
		onPicked(filepath.Clean(filepath.FromSlash(uri.Path())))
	}, w)
	d.SetFilter(storage.NewExtensionFileFilter([]string{".zip"}))

	if dir := filepath.Dir(current); current != "" && isDir(dir) {
		if loc, err := storage.ListerForURI(storage.NewFileURI(dir)); err == nil {
			d.SetLocation(loc)
		}
	}
	d.Show()
}

// confirmUninstall 弹出卸载确认框，并让用户选择是否保留个人数据。
func confirmUninstall(w fyne.Window, onConfirmed func(keepUserData bool)) {
	note := widget.NewLabel("将删除 KfuPet 的程序文件、快捷方式与安装信息。")
	note.Wrapping = fyne.TextWrapWord

	keepData := widget.NewCheck("保留个人数据（配置、角色等）", nil)
	keepData.SetChecked(true) // 默认保留，避免误删

	dialog.ShowCustomConfirm("卸载 KfuPet", "卸载", "取消",
		container.NewVBox(note, keepData),
		func(confirmed bool) {
			if confirmed {
				onConfirmed(keepData.Checked)
			}
		}, w)
}

// confirmInstallEnv 在缺少运行环境时询问是否一并安装，默认勾选。
// 「继续」带着勾选结果进入安装向导，「取消」则放弃本次安装、留在主界面。
func confirmInstallEnv(w fyne.Window, onChoice func(withEnv bool)) {
	note := widget.NewLabel("未检测到 KfuPet 所需的运行环境（" + dotnet.DisplayName + "），缺少它 KfuPet 无法启动。")
	note.Wrapping = fyne.TextWrapWord

	withEnv := widget.NewCheck("一并安装运行环境（推荐）", nil)
	withEnv.SetChecked(true)

	dialog.ShowCustomConfirm("缺少运行环境", "继续", "取消",
		container.NewVBox(note, withEnv),
		func(confirmed bool) {
			if confirmed {
				onChoice(withEnv.Checked)
			}
		}, w)
}

// showEnvManualDownload 在运行环境自动安装失败（候选下载地址均不可用）时，
// 引导用户自行下载安装：官网与蓝奏云二选一，或稍后自行处理。
func showEnvManualDownload(w fyne.Window) {
	note := widget.NewLabel("运行环境未能自动下载安装，请手动安装后再启动 KfuPet。")
	note.Wrapping = fyne.TextWrapWord
	code := smallText("蓝奏云提取码：" + dotnet.LanzouCode)

	var d *dialog.CustomDialog
	// 三个选项都会关掉本弹窗：前两个再拉起浏览器。
	open := func(raw string) {
		d.Hide()
		if u, err := url.Parse(raw); err == nil {
			_ = fyne.CurrentApp().OpenURL(u)
		}
	}
	buttons := container.NewHBox(
		widget.NewButton("微软官网", func() { open(dotnet.OfficialURL) }),
		widget.NewButton("蓝奏云", func() { open(dotnet.LanzouURL) }),
		widget.NewButton("稍后自行安装", func() { d.Hide() }),
	)
	d = dialog.NewCustomWithoutButtons("需要手动安装运行环境",
		container.NewVBox(note, code, container.NewCenter(buttons)), w)
	d.Show()
}

// formatBytes 把字节数格式化为便于阅读的形式，如 "67.4 MB"。
func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	value, exp := float64(n), 0
	for value >= unit && exp < 4 {
		value /= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", value, "KMGT"[exp-1])
}

const minSplashDuration = 2 * time.Second

// 闪屏收尾的 Logo 衔接：闪屏 Logo 先渐隐，切到主界面后左上角 Logo 再渐显。
const (
	logoFadeOutDuration = 260 * time.Millisecond
	logoFadeInDuration  = 450 * time.Millisecond
)

// checkingView 组装查询中的全屏闪屏：Logo + 旋转指示器 + 跳动省略号。
// 返回内容连同 Logo 的遮罩，供查询结束后播放 Logo 渐隐。
func checkingView() (fyne.CanvasObject, *uifx.Cover) {
	logoImage := canvas.NewImageFromResource(fyne.NewStaticResource("Startlogo.png", startLogoPNG))
	logoImage.FillMode = canvas.ImageFillContain
	logoArea, logoCover := uifx.NewCover(logoImage)
	logoBox := container.NewCenter(container.NewGridWrap(fyne.NewSize(96, 96), logoArea))

	spinner := uifx.NewSpinner()

	label := widget.NewLabelWithStyle("正在查询当前版本信息", fyne.TextAlignCenter, fyne.TextStyle{})
	dots := uifx.NewDots()
	appName := smallText("KfuPetInstall")

	return container.NewCenter(container.NewVBox(
		logoBox,
		container.NewCenter(container.NewGridWrap(fyne.NewSize(40, 40), spinner)),
		container.NewCenter(container.NewHBox(label, dots)),
		container.NewCenter(appName),
	)), logoCover
}

// buildFailedPanel 组装右侧的失败面板，用于查询失败与安装失败两种情形。
// 不整屏报错，使左键的可用操作不受影响。
func buildFailedPanel(title string, failure error, retryLabel string, onRetry func()) fyne.CanvasObject {
	titleLabel := widget.NewLabelWithStyle(title, fyne.TextAlignCenter, fyne.TextStyle{Bold: true})

	// 透出真实失败原因
	detail := widget.NewLabel("原因：" + failure.Error())
	detail.Alignment = fyne.TextAlignCenter
	detail.Wrapping = fyne.TextWrapWord

	retry := actionButton(retryLabel, onRetry)

	return container.NewVBox(
		layout.NewSpacer(),
		titleLabel,
		detail,
		container.NewCenter(retry),
		layout.NewSpacer(),
	)
}

// instanceLockTimeout 是抢单实例锁的最长等待。
// 交棒时原进程会先放锁，通常毫秒级就能拿到，这个窗口只是覆盖那点空档；
// 也不能太长——用户重复双击图标时要尽快得到"已在运行"的结论。
const instanceLockTimeout = 3 * time.Second

func main() {
	os.Exit(run())
}

// run 是真正的入口，返回进程退出码。
// 用返回值而不是就地 os.Exit，是为了让 defer（放锁、安排临时副本自删）都能执行。
func run() int {
	cmd := parseArgs(os.Args[1:])
	cleanStaleTempDirs() // 兜底清理崩溃/被强杀留下的临时副本目录

	// 同一时刻只允许一个实例：两个实例同时安装/卸载会争抢同一个安装目录，
	// 而且常驻副本正在运行时，别的实例既替换不了也删不掉安装目录。
	if err := winapi.AcquireInstanceLock(instanceLockTimeout); err != nil {
		winapi.NotifyError("KfuPet 无法启动", err.Error())
		return 1
	}
	defer winapi.ReleaseInstanceLock()
	// 若自身是交棒过来的临时副本，退出后把自己的目录也删掉。
	defer removeTempDirLater()

	// 要删除/替换安装目录的动作，若自身正运行于该目录内（常驻副本），
	// 先交棒给临时副本：运行中的 exe 映像被占用，不搬走就会卡住整个操作。
	// （relayToTemp 内部会先放锁，副本才拿得到。）
	if cmd.modifiesInstallDir() {
		if dir := resolveInstallDir(cmd); dir != "" && isSelfWithin(dir) {
			if err := relayToTemp(cmd); err != nil {
				winapi.NotifyError("KfuPet 无法继续", err.Error())
				return 1
			}
			return 0
		}
	}

	// 升级（--action=update）走图形界面：进界面后直接进升级流程（见 runGUI）。
	// 安装位置取不到就没得升，此时还没有窗口可用，只能弹系统提示。
	if cmd.Action == actionUpdate && resolveInstallDir(cmd) == "" {
		winapi.NotifyError("KfuPet 升级失败", "未找到 KfuPet 的安装位置，无法升级。")
		return 1
	}

	// 静默卸载：不带界面，直接删程序文件、快捷方式与安装信息。
	if cmd.Action == actionUninstall && cmd.Yes {
		if err := runSilentUninstall(cmd); err != nil {
			winapi.NotifyError("KfuPet 卸载失败", err.Error())
			return 1
		}
		return 0
	}

	runGUI(cmd)
	return 0
}

// runGUI 启动图形界面。
// 传 --action=uninstall 时会直接弹出卸载确认框，省去在主界面再点一次。
func runGUI(cmd command) {
	a := app.New()
	// 窗口图标交给 exe 里由 app.rc 链入的多尺寸图标（含 16/20/24/32 等）。
	// 这里若再设单张位图，Fyne 会按原图尺寸交给系统，标题栏/任务栏要 16/32 时
	// 只能把它缩小，反而变糊。

	w := a.NewWindow("KfuPetInstall")
	w.Resize(fyne.NewSize(600, 420))
	w.SetFixedSize(true)
	w.CenterOnScreen()

	checker := newUpdateChecker()

	var state appState
	var render func()
	var startVersionCheck func()
	var startInstall func()
	var startUpgrade func(dir string)
	var startRepair func()
	var runRepair func(plan repairPlan)
	var startUninstall func(keepUserData bool)
	var installing *installingView // 进行中页面（安装/升级/修复）：原地更新，不随 render 重建
	var upgradeDir string          // 本次升级的目标安装目录，失败重试时复用
	uninstallPrompted := false     // 标准卸载入口只自动弹一次确认框，避免重试查询时重复弹出

	// 闪屏 Logo 渐隐结束后，主界面左上角 Logo 接着渐显；为真时下一次渲染走这条衔接。
	logoFadePending := false

	// backToMain 回主界面。由桌宠拉起（--action=update）的升级没做过版本查询，
	// 而主界面要展示版本信息，此时直接渲染是空的，得先补一次查询。
	backToMain := func() {
		if state.rel == nil {
			startVersionCheck()
			return
		}
		state.phase = phaseIdle
		render()
	}

	// 页面切换时整页淡入；同一阶段内的重建（如勾选选项）不重复播放。
	lastPhase := installPhase(-1)
	setContent := func(c fyne.CanvasObject) {
		if state.phase != lastPhase {
			c = uifx.FadeIn(c, 280*time.Millisecond)
			lastPhase = state.phase
		}
		w.SetContent(c)
	}

	render = func() {
		// 安装向导与升级/修复流程期间整屏切换到流程页，结束后再回到主界面。
		if state.phase != phaseIdle {
			// 体检页是静态的（转圈 + 文案），单独处理。
			if state.phase == phaseRepairChecking {
				setContent(buildRepairCheckingView())
				return
			}
			if state.phase == phaseRunning || state.phase == phaseUpgrading || state.phase == phaseRepairing {
				if installing == nil {
					// 进行中的页面在原地更新，不随 render 重建：步骤画勾、流光等
					// 动画才能连续播放。
					switch state.phase {
					case phaseUpgrading:
						installing = newUpgradingView(state)
					case phaseRepairing:
						installing = newRepairingView(state)
					default:
						installing = newInstallingView("KfuPet 安装中", stagesFor(state.opts), state)
					}
				}
				setContent(installing.root)
				return
			}
			// 升级与修复的结果页不属于安装向导，单独分流。
			if state.phase == phaseUpgradeDone {
				if state.waitKfuPet {
					// 桌宠拉起的那条路径没人守着界面，升完自动把桌宠拉回来。
					setContent(buildUpgradeDoneView(state.st.Version, upgradeDir))
					return
				}
				// 手动升级：同安装一样问一句是否立即启动。
				setContent(buildDoneView("升级完成", state.st.Version, upgradeDir, state.warnings,
					func() {
						if err := launchKfuPet(upgradeDir); err != nil {
							dialog.ShowError(err, w)
							return
						}
						a.Quit()
					},
					func() { a.Quit() }))
				return
			}
			if state.phase == phaseUpgradeFailed {
				setContent(buildInstallFailedView("升级失败", state.installErr, upgradeDir,
					func() { startUpgrade(upgradeDir) }, backToMain))
				return
			}
			if state.phase == phaseRepairDone {
				setContent(buildRepairDoneView(state.st.Path, state.st.Version, state.repair.summary(),
					state.warnings,
					func() {
						if err := launchKfuPet(state.st.Path); err != nil {
							dialog.ShowError(err, w)
							return
						}
						a.Quit()
					},
					func() { a.Quit() }))
				return
			}
			if state.phase == phaseRepairFailed {
				// 重试重做一遍体检：失败后现场可能已变，按旧结论重跑并不安全。
				setContent(buildInstallFailedView("修复失败", state.installErr, state.st.Path,
					func() { startRepair() }, backToMain))
				return
			}
			setContent(buildInstallView(state, flowHandlers{
				browse: func() {
					pickInstallDir(w, state.targetDir, func(dir string) {
						state.targetDir = dir
						render()
					})
				},
				next: func() {
					if err := validateInstallDir(state.targetDir); err != nil {
						dialog.ShowError(err, w)
						return
					}
					state.phase = phaseOptions
					render()
				},
				back: func() {
					state.phase = phaseChooseDir
					render()
				},
				install: func() {
					// 离线安装必须先选定可用的安装包，否则留在本页提示。
					if err := validateInstallOptions(state.opts); err != nil {
						dialog.ShowError(err, w)
						return
					}
					startInstall()
				},
				retry: func() { startInstall() },
				backToMain: func() {
					state.phase = phaseIdle
					render()
				},
				launch: func() {
					if err := launchKfuPet(state.targetDir); err != nil {
						// 启动失败就留在完成页，用户可以再点一次或直接选「否」
						dialog.ShowError(err, w)
						return
					}
					a.Quit()
				},
				quit:         func() { a.Quit() },
				setDesktop:   func(checked bool) { state.opts.Desktop = checked },
				setStartMenu: func(checked bool) { state.opts.StartMenu = checked },
				setOffline: func(offline bool) {
					state.opts.Offline = offline
					render()
				},
				pickPackage: func() {
					pickInstallPackage(w, state.opts.Package, func(path string) {
						state.opts.Package = path
						render()
					})
				},
			}))
			return
		}

		main, logoCover := buildMainUI(state, uiHandlers{
			retryCheck: func() { startVersionCheck() },
			install: func() {
				// 进向导第一步：预填默认目录，快捷方式默认都勾选。
				begin := func(withEnv bool) {
					state.targetDir = defaultInstallDir()
					state.opts = installOptions{Desktop: true, StartMenu: true, InstallEnv: withEnv}
					state.phase = phaseChooseDir
					render()
				}
				// 缺少运行环境（.NET 桌面运行时）时先询问是否一并安装，默认勾选；
				// 用户取消则放弃本次安装，留在主界面。
				if state.env.Present {
					begin(false)
					return
				}
				confirmInstallEnv(w, begin)
			},
			repair:    func() { startRepair() },
			uninstall: func() { confirmUninstall(w, startUninstall) },
		})
		if logoFadePending {
			// 接在闪屏 Logo 渐隐之后：这里先遮住 Logo，切页后再让它渐显。
			// 这次切换不再整页淡入，观感由 Logo 的渐隐—渐显主导。
			logoFadePending = false
			logoCover.Conceal()
			lastPhase = state.phase
			setContent(main)
			logoCover.FadeIn(logoFadeInDuration)
			return
		}
		setContent(main)
	}

	// 打开后：先显示转圈圈闪屏查询当前版本信息，
	// 完成后切到主界面展示最新版本；查询失败时同样进主界面，
	// 只在右侧面板展示失败原因与重试，不整屏报错。
	startVersionCheck = func() {
		state.phase = phaseIdle
		state.checkErr = nil
		splash, splashLogo := checkingView()
		w.SetContent(uifx.FadeIn(splash, 280*time.Millisecond))
		lastPhase = -1 // 闪屏之后的首次渲染也要淡入

		shownAt := time.Now() // 记录闪屏开始时刻，用于保证最短展示时长
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
			defer cancel()

			// 本地注册表与运行环境检查都很快，与远端查询一并在后台完成，避免阻塞界面。
			st := detectInstallState()
			env := dotnet.Detect()
			rel, err := checker.check(ctx)

			// 查询提前完成时，补足剩余时长再切换，避免闪屏一闪而过
			if remain := minSplashDuration - time.Since(shownAt); remain > 0 {
				time.Sleep(remain)
			}

			fyne.Do(func() {
				state.st = st
				state.env = env
				state.rel = rel
				state.checkErr = err
				// 闪屏 Logo 先渐隐，落幕后切主界面，左上角 Logo 接着渐显。
				splashLogo.FadeOut(logoFadeOutDuration, func() {
					logoFadePending = true
					render()
					// 由标准卸载入口拉起时，直接进卸载确认，省得用户在主界面再点一次。
					if cmd.Action == actionUninstall && state.st.Installed && !uninstallPrompted {
						uninstallPrompted = true
						confirmUninstall(w, startUninstall)
					}
				})
			})
		}()
	}

	// 安装：把当前发布版按向导选定的目录与选项安装。
	// 在线安装依赖发布信息；离线安装直接用本地包，不依赖网络。
	startInstall = func() {
		rel := state.rel
		if state.phase == phaseRunning {
			return
		}
		if rel == nil && !state.opts.Offline {
			return
		}
		installDir, opts := state.targetDir, state.opts

		// 运行环境可能在本次会话里已被装好（例如用户刚手动装过），不必重复下载。
		if opts.InstallEnv && dotnet.Detect().Present {
			opts.InstallEnv = false
			state.opts.InstallEnv = false
		}

		state.phase = phaseRunning
		state.installErr = nil
		state.warnings = nil
		// 进度起点：要先装运行环境就从它开始；离线安装跳过下载，从校验阶段起步。
		firstStage := stageDownloading
		switch {
		case opts.InstallEnv:
			firstStage = stageEnvDownloading
		case opts.Offline:
			firstStage = stageVerifying
		}
		state.progress = installProgress{Stage: firstStage}
		installing = nil // 强制重建安装中页面（步骤数随选项变化）
		render()

		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), installTimeout)
			defer cancel()

			res, err := installKfuPet(ctx, rel, installDir, opts, func(p installProgress) {
				fyne.Do(func() {
					state.progress = p
					// 原地更新，不整页重建：步骤画勾、流光等动画才能连续播放
					if installing != nil {
						installing.update(p)
					}
				})
			})

			fyne.Do(func() {
				if err != nil {
					state.phase = phaseFailed
					state.installErr = err
				} else {
					state.phase = phaseDone
					state.st = res.state
					state.warnings = res.warnings
				}
				installing = nil
				render()
				// 运行环境没能自动装好（下载地址均不可用）时，安装结束引导用户手动下载。
				if err == nil && res.envSkipped {
					showEnvManualDownload(w)
				}
			})
		}()
	}

	// 升级：等桌宠让位（由 --wait-pid 指定）→ 查最新版本 → 版本比较 → 需要时整体安装。
	// 目录沿用注册表记录，不问位置与选项；完成后自动拉起新版 KfuPet 并退出。
	startUpgrade = func(dir string) {
		if state.phase == phaseUpgrading {
			return
		}
		upgradeDir = dir
		state.waitKfuPet = len(cmd.WaitPIDs) > 0
		state.phase = phaseUpgrading
		state.installErr = nil
		// 升级不建快捷方式，正常不会有警告；清一下是为了不把上一轮流程的提示带过来。
		state.warnings = nil
		state.progress = installProgress{Stage: upgradeStages(state.waitKfuPet)[0]}
		installing = nil // 强制重建进行中页面（步骤清单与安装不同）
		render()

		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), installTimeout)
			defer cancel()

			out, err := upgradeKfuPet(ctx, checker, dir, cmd.WaitPIDs, killConfirm(w), func(p installProgress) {
				fyne.Do(func() {
					state.progress = p
					// 原地更新，不整页重建：步骤画勾、流光等动画才能连续播放
					if installing != nil {
						installing.update(p)
					}
				})
			})

			fyne.Do(func() {
				installing = nil
				switch {
				case err != nil:
					state.phase = phaseUpgradeFailed
					state.installErr = err
					render()
				case !out.check.NeedUpgrade():
					// 没有可升级的版本：先说清是「已是最新」还是「本机比线上新」，
					// 再回主界面（重新检测安装状态，避免残留旧值）。
					state.st = detectInstallState()
					backToMain()
					showUpgradeSkipped(w, out.check)
				default:
					state.st = out.result.state
					state.phase = phaseUpgradeDone
					render()
					// 只有桌宠拉起的那条路径自动重启桌宠；手动升级停在完成页让用户自己选。
					if state.waitKfuPet {
						scheduleRelaunch(a, w, state.st.Path)
					}
				}
			})
		}()
	}

	// 修复：体检 → 报告并确认 → 缺什么补什么。
	// 与安装/升级不同，修复不替换整个安装目录，只覆盖校验不通过的文件，
	// 因此不需要交棒临时副本，也不会动用户自己加进安装目录的内容。
	startRepair = func() {
		installDir, localVer := state.st.Path, state.st.Version
		if installDir == "" {
			return
		}
		state.phase = phaseRepairChecking
		state.installErr = nil
		render()

		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), installTimeout)
			defer cancel()

			plan := inspectInstall(ctx, checker, installDir, localVer, nil)

			fyne.Do(func() {
				// 要覆盖 KfuPet 的程序文件时它必须先退出：正在运行的映像替换不掉。
				// 只补常驻副本/快捷方式/安装信息时不碰这些文件，不必拦下用户。
				if plan.needsKfuPetClosed() && kfuPetRunning(installDir) {
					state.phase = phaseIdle
					render()
					dialog.ShowError(fmt.Errorf("KfuPet 正在运行，请先退出后再修复"), w)
					return
				}
				if !plan.needsWork() {
					state.phase = phaseIdle
					render()
					// 本地项都正常、只是程序文件没校验成（无网等）时，别说成"无需修复"。
					if len(plan.coreMissing) > 0 || plan.coreSkipReason != "" {
						showRepairIncomplete(w, plan)
					} else {
						showNothingToRepair(w, plan)
					}
					return
				}
				confirmRepair(w, plan, func(start bool) {
					if !start {
						state.phase = phaseIdle
						render()
						return
					}
					runRepair(plan)
				})
			})
		}()
	}

	runRepair = func(plan repairPlan) {
		state.repair = plan
		state.phase = phaseRepairing
		state.installErr = nil
		state.warnings = nil
		state.progress = installProgress{Stage: repairStagesFor(plan.needsDownload())[0]}
		installing = nil // 强制重建进行中页面（步骤清单按是否需要下载而定）
		render()

		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), installTimeout)
			defer cancel()

			res, err := repairKfuPet(ctx, plan, func(p installProgress) {
				fyne.Do(func() {
					state.progress = p
					// 原地更新，不整页重建：步骤画勾、流光等动画才能连续播放
					if installing != nil {
						installing.update(p)
					}
				})
			})

			fyne.Do(func() {
				installing = nil
				if err != nil {
					state.phase = phaseRepairFailed
					state.installErr = err
					render()
					return
				}
				// 重新检测：修复可能改过版本号，主界面要如实反映现状。
				state.repair = res.plan
				state.st = detectInstallState()
				state.warnings = res.warnings
				state.phase = phaseRepairDone
				render()
			})
		}()
	}

	// 卸载：确认之后删除程序文件、快捷方式与注册表记录。
	startUninstall = func(keepUserData bool) {
		installDir := state.st.Path
		if installDir == "" {
			return
		}

		// 本程序就住在安装目录里时（常驻副本），它删不掉自己正在运行的文件：
		// 交棒给临时副本静默卸载，本界面随即退出；结果由副本弹窗告知。
		if isSelfWithin(installDir) {
			err := relayToTemp(command{
				Action:    actionUninstall,
				Dir:       installDir,
				Yes:       true,
				PurgeData: !keepUserData,
				Notify:    true,
			})
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			a.Quit()
			return
		}

		// 卸载期间用模态框挡住主界面，避免重复触发。
		spinner := uifx.NewSpinner()
		busyLabel := widget.NewLabel("正在卸载 KfuPet")
		modal := dialog.NewCustomWithoutButtons("正在卸载",
			container.NewCenter(container.NewVBox(
				container.NewCenter(container.NewGridWrap(fyne.NewSize(48, 48), spinner)),
				container.NewCenter(container.NewHBox(busyLabel, uifx.NewDots())),
			)), w)
		modal.Show()

		go func() {
			warnings, err := uninstallKfuPet(installDir, uninstallOptions{KeepUserData: keepUserData})

			fyne.Do(func() {
				modal.Hide()
				// 重新检测：成功则主界面回到"仅安装"，失败也如实反映磁盘现状。
				state.st = detectInstallState()
				render()
				if err != nil {
					dialog.ShowError(err, w)
					return
				}
				// 降级为警告的问题（如快捷方式没删掉）一并说明，别让用户以为清理得很干净。
				msg := "KfuPet 已卸载。"
				if len(warnings) > 0 {
					msg += "\n\n" + strings.Join(warnings, "\n")
				}
				dialog.ShowInformation("卸载完成", msg, w)
			})
		}()
	}

	// 由桌宠拉起（--action=update）时跳过主界面与闪屏：用户是在桌宠里点的「立即更新」，
	// 再看一遍主界面没有意义。但动手前要先问一句——升级会重新下载安装包并整体替换
	// 安装目录，不该在用户没点头的情况下就开始。
	if cmd.Action == actionUpdate {
		dir := resolveInstallDir(cmd)
		w.SetContent(uifx.FadeIn(buildUpdateReadyView(), 280*time.Millisecond))
		// 先让窗口可见再弹确认框，否则弹窗会挂在一个还没显示的窗口上。
		w.Show()
		confirmUpgrade(w, func(continueUpdate bool) {
			if !continueUpdate {
				// 用户改主意了：本次直接退出，不碰安装目录，也不去动桌宠。
				a.Quit()
				return
			}
			startUpgrade(dir)
		})
		a.Run()
		return
	}

	startVersionCheck()
	w.ShowAndRun()
}
