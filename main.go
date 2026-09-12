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
	"runtime"
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

	"kfupet-installer/internal/winapi"
)

//go:embed icon/Startlogo.png
var startLogoPNG []byte

//go:embed icon/appicon.png
var appIconPNG []byte

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
	phaseIdle      installPhase = iota // 不在安装流程中，展示主界面
	phaseChooseDir                     // 向导第一步：选择安装位置
	phaseOptions                       // 向导第二步：安装选项
	phaseRunning                       // 正在安装
	phaseDone                          // 安装完成
	phaseFailed                        // 安装失败
)

// appState 是界面的全部可变状态；每次变化后整体重建界面。
type appState struct {
	rel      *releaseInfo // 远端发布信息；查询失败时为 nil
	checkErr error        // 版本查询失败原因
	st       installState // 本机安装状态

	phase      installPhase
	targetDir  string          // 本次安装的目标目录
	opts       installOptions  // 本次安装的选项
	progress   installProgress // 最近一次进度汇报
	installErr error           // 安装失败原因
}

// uiHandlers 是主界面各操作入口。
type uiHandlers struct {
	retryCheck func()
	install    func()
	upgrade    func()
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
}

// buildMainUI 组装主界面。
// 可用操作由注册表安装状态决定：未安装只提供「安装」，
// 已安装提供「升级」「卸载」，「安装」不再出现。
func buildMainUI(s appState, h uiHandlers) fyne.CanvasObject {
	var actions []fyne.CanvasObject
	if s.st.Installed {
		actions = append(actions,
			widget.NewButton("升级", h.upgrade),
			widget.NewButton("卸载", h.uninstall),
		)
	} else {
		actions = append(actions, widget.NewButton("安装", h.install))
	}

	// 上方图标：从资源嵌入的 KfuPet Logo
	logoImage := canvas.NewImageFromResource(fyne.NewStaticResource("Startlogo.png", startLogoPNG))
	logoImage.FillMode = canvas.ImageFillContain
	logoArea := container.NewCenter(container.NewGridWrap(fyne.NewSize(120, 120), logoImage))

	// 左侧：上方图标 + 下方按钮（数量随安装状态变化）
	buttons := container.New(&vfillLayout{
		width:      180,
		itemHeight: 48,
		spacing:    20,
	}, actions...)
	left := container.NewBorder(logoArea, nil, nil, nil, buttons)

	return container.NewBorder(nil, bottomBar(), left, nil, buildRightPanel(s, h))
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
func buildInstallView(s appState, h flowHandlers) fyne.CanvasObject {
	switch s.phase {
	case phaseChooseDir:
		return buildChooseDirPage(s, h)
	case phaseOptions:
		return buildOptionsPage(s, h)
	case phaseDone:
		return buildInstallDoneView(s, h)
	case phaseFailed:
		return buildInstallFailedView(s, h)
	default:
		return buildInstallingView(s)
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

// buildOptionsPage 向导第二步：选择是否创建快捷方式。
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

	return container.NewVBox(
		layout.NewSpacer(),
		container.NewCenter(caption),
		container.NewCenter(title),
		container.NewCenter(desktopCheck),
		container.NewCenter(startMenuCheck),
		container.NewCenter(smallText("安装位置："+s.targetDir)),
		container.NewCenter(container.NewHBox(
			actionButton("上一步", h.back),
			actionButton("安装", h.install),
		)),
		layout.NewSpacer(),
	)
}

// buildInstallingView 展示安装进行中的步骤清单与下载进度。
func buildInstallingView(s appState) fyne.CanvasObject {
	caption := smallText("KfuPet 安装中")
	caption.TextStyle = fyne.TextStyle{}

	stageText := canvas.NewText(string(s.progress.Stage), theme.Color(theme.ColorNameForeground))
	stageText.TextSize = 20
	stageText.TextStyle = fyne.TextStyle{Bold: true}

	// 下载阶段总量已知：用确定进度条并展示速度与字节数；
	// 其余阶段无法量化，用不确定进度条表示"正在进行"。
	// 进度条自身会渲染百分比文案，宽度需由外部约束。
	var bar fyne.CanvasObject
	detail := " "
	if s.progress.Stage == stageDownloading && s.progress.Total > 0 {
		progressBar := widget.NewProgressBar()
		progressBar.Max = float64(s.progress.Total)
		progressBar.SetValue(float64(s.progress.Done))
		bar = progressBar
		detail = fmt.Sprintf("%s / %s　%s/s",
			formatBytes(s.progress.Done),
			formatBytes(s.progress.Total),
			formatBytes(int64(s.progress.Speed)))
	} else {
		progressBar := widget.NewProgressBarInfinite()
		progressBar.Start()
		bar = progressBar
	}

	return container.NewCenter(container.NewVBox(
		container.NewCenter(caption),
		container.NewCenter(stageText),
		container.NewGridWrap(fyne.NewSize(360, 26), bar),
		container.NewCenter(smallText(detail)),
		buildStepList(stagesFor(s.opts), s.progress.Stage),
		container.NewCenter(smallText("安装位置："+s.targetDir)),
	))
}

// buildStepList 组装安装步骤清单：已完成用常规色、当前步骤高亮、未开始弱化。
func buildStepList(stages []installStage, current installStage) fyne.CanvasObject {
	idx := stageIndex(stages, current)
	rows := make([]fyne.CanvasObject, 0, len(stages))
	for i, stage := range stages {
		label := widget.NewLabel(fmt.Sprintf("%d. %s", i+1, stage))
		switch {
		case i < idx:
			label.Importance = widget.MediumImportance
		case i == idx:
			label.Importance = widget.HighImportance
			label.TextStyle = fyne.TextStyle{Bold: true}
		default:
			label.Importance = widget.LowImportance
		}
		rows = append(rows, container.NewCenter(label))
	}
	return container.NewVBox(rows...)
}

// buildInstallDoneView 展示安装结果，并询问是否立即启动。
// 两个选项都会退出 updater：安装这一步已经完成，没有留在界面上的必要。
func buildInstallDoneView(s appState, h flowHandlers) fyne.CanvasObject {
	title := canvas.NewText("安装完成", theme.Color(theme.ColorNameForeground))
	title.TextSize = 24
	title.TextStyle = fyne.TextStyle{Bold: true}

	version := " "
	if s.st.Version != "" {
		version = "KfuPet " + s.st.Version
	}

	question := widget.NewLabel("是否立即启动 KfuPet？")
	question.Alignment = fyne.TextAlignCenter

	return container.NewVBox(
		layout.NewSpacer(),
		container.NewCenter(title),
		container.NewCenter(smallText(version)),
		container.NewCenter(smallText("安装位置："+s.targetDir)),
		container.NewCenter(question),
		container.NewCenter(container.NewHBox(
			actionButton("是", h.launch),
			actionButton("否", h.quit),
		)),
		layout.NewSpacer(),
	)
}

// buildInstallFailedView 展示安装失败原因与重试、返回入口。
func buildInstallFailedView(s appState, h flowHandlers) fyne.CanvasObject {
	title := widget.NewLabelWithStyle("安装失败", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})

	reason := "未知错误"
	if s.installErr != nil {
		reason = s.installErr.Error()
	}
	detail := widget.NewLabel("原因：" + reason)
	detail.Alignment = fyne.TextAlignCenter
	detail.Wrapping = fyne.TextWrapWord

	return container.NewVBox(
		layout.NewSpacer(),
		title,
		detail,
		container.NewCenter(smallText("安装位置："+s.targetDir)),
		container.NewCenter(container.NewHBox(
			actionButton("重试", h.retry),
			actionButton("返回", h.backToMain),
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

// checkingView 组装查询中的全屏闪屏查询当前版本信息。
func checkingView() fyne.CanvasObject {
	activity := widget.NewActivity()
	activity.Start()

	label := widget.NewLabelWithStyle("正在查询当前版本信息…", fyne.TextAlignCenter, fyne.TextStyle{})
	appName := smallText("KfuPetUpdate")

	return container.NewCenter(container.NewVBox(
		container.NewCenter(container.NewGridWrap(fyne.NewSize(48, 48), activity)),
		container.NewCenter(label),
		container.NewCenter(appName),
	))
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

	// 升级（--action=update）目前只预留入口：参数能解析、分支能进来，但不执行真实升级。
	// 待版本比较（1.md 第二节）与桌宠侧拉起约定落地后再接上：
	//   TODO: 等 --wait-pid 指定的进程退出 → 查询最新版 → 与本地版本比较 →
	//         需要时整体安装，并在安装后重新放置安装目录内的常驻副本。
	if cmd.Action == actionUpdate {
		winapi.NotifyInfo("KfuPet 更新", "升级功能尚未实现，请先使用安装向导完成安装。")
		return 0
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
	// 窗口图标：Windows 交给 exe 里由 app.rc 链入的多尺寸图标（含 16/20/24/32 等）。
	// 这里若再设单张位图，Fyne 会按原图尺寸交给系统，标题栏/任务栏要 16/32 时
	// 只能把它缩小，反而变糊；其他平台没有 exe 资源，仍用内嵌 PNG。
	if runtime.GOOS != "windows" {
		a.SetIcon(fyne.NewStaticResource("appicon.png", appIconPNG))
	}

	w := a.NewWindow("KfuPetUpdate")
	w.Resize(fyne.NewSize(600, 360))
	w.SetFixedSize(true)
	w.CenterOnScreen()

	checker := newUpdateChecker()

	var state appState
	var render func()
	var startVersionCheck func()
	var startInstall func()
	var startUninstall func(keepUserData bool)
	uninstallPrompted := false // 标准卸载入口只自动弹一次确认框，避免重试查询时重复弹出

	render = func() {
		// 安装流程期间整屏切换到安装向导页，结束后再回到主界面。
		if state.phase != phaseIdle {
			w.SetContent(buildInstallView(state, flowHandlers{
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
				install: func() { startInstall() },
				retry:   func() { startInstall() },
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
			}))
			return
		}

		w.SetContent(buildMainUI(state, uiHandlers{
			retryCheck: func() { startVersionCheck() },
			install: func() {
				// 进向导第一步：预填默认目录，快捷方式默认都勾选。
				state.targetDir = defaultInstallDir()
				state.opts = installOptions{Desktop: true, StartMenu: true}
				state.phase = phaseChooseDir
				render()
			},
			upgrade: func() {
				// TODO: 升级逻辑（沿用注册表记录的安装目录，不再询问）
			},
			uninstall: func() { confirmUninstall(w, startUninstall) },
		}))
	}

	// 打开后：先显示转圈圈闪屏查询当前版本信息，
	// 完成后切到主界面展示最新版本；查询失败时同样进主界面，
	// 只在右侧面板展示失败原因与重试，不整屏报错。
	startVersionCheck = func() {
		state.phase = phaseIdle
		state.checkErr = nil
		w.SetContent(checkingView())

		shownAt := time.Now() // 记录闪屏开始时刻，用于保证最短展示时长
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
			defer cancel()

			// 本地注册表检查很快，与远端查询一并在后台完成，避免阻塞界面。
			st := detectInstallState()
			rel, err := checker.check(ctx)

			// 查询提前完成时，补足剩余时长再切换，避免闪屏一闪而过
			if remain := minSplashDuration - time.Since(shownAt); remain > 0 {
				time.Sleep(remain)
			}

			fyne.Do(func() {
				state.st = st
				state.rel = rel
				state.checkErr = err
				render()
				// 由标准卸载入口拉起时，直接进卸载确认，省得用户在主界面再点一次。
				if cmd.Action == actionUninstall && state.st.Installed && !uninstallPrompted {
					uninstallPrompted = true
					confirmUninstall(w, startUninstall)
				}
			})
		}()
	}

	// 安装：把当前发布版按向导选定的目录与选项安装。
	startInstall = func() {
		rel := state.rel
		if rel == nil || state.phase == phaseRunning {
			return
		}
		installDir, opts := state.targetDir, state.opts

		state.phase = phaseRunning
		state.installErr = nil
		state.progress = installProgress{Stage: stageDownloading}
		render()

		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), installTimeout)
			defer cancel()

			st, err := installKfuPet(ctx, rel, installDir, opts, func(p installProgress) {
				fyne.Do(func() {
					state.progress = p
					render()
				})
			})

			fyne.Do(func() {
				if err != nil {
					state.phase = phaseFailed
					state.installErr = err
				} else {
					state.phase = phaseDone
					state.st = st
				}
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
		activity := widget.NewActivity()
		activity.Start()
		modal := dialog.NewCustomWithoutButtons("正在卸载",
			container.NewCenter(container.NewVBox(
				container.NewCenter(container.NewGridWrap(fyne.NewSize(48, 48), activity)),
				container.NewCenter(widget.NewLabel("正在卸载 KfuPet…")),
			)), w)
		modal.Show()

		go func() {
			err := uninstallKfuPet(installDir, uninstallOptions{KeepUserData: keepUserData})

			fyne.Do(func() {
				modal.Hide()
				// 重新检测：成功则主界面回到"仅安装"，失败也如实反映磁盘现状。
				state.st = detectInstallState()
				render()
				if err != nil {
					dialog.ShowError(err, w)
					return
				}
				dialog.ShowInformation("卸载完成", "KfuPet 已卸载。", w)
			})
		}()
	}

	startVersionCheck()
	w.ShowAndRun()
}
