package main

import (
	"context"
	_ "embed"
	"image/color"
	_ "image/png"
	"net/url"
	"runtime"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
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

// buildVersionPanel 组装右侧信息面板：
// 查询成功时展示远端最新版本；查询失败时在同样的位置展示失败原因与重试入口。
func buildVersionPanel(rel *releaseInfo, checkErr error, st installState, onRetry func()) fyne.CanvasObject {
	if checkErr != nil {
		return buildCheckFailedPanel(checkErr, onRetry)
	}

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

// buildMainUI 组装主界面。
// 可用操作由注册表安装状态决定：未安装只提供「安装」，
// 已安装提供「升级」「卸载」，「安装」不再出现。
func buildMainUI(rel *releaseInfo, checkErr error, st installState, onRetry func()) fyne.CanvasObject {
	var actions []fyne.CanvasObject
	if st.Installed {
		actions = append(actions,
			widget.NewButton("升级", func() {
				// TODO: 升级逻辑
			}),
			widget.NewButton("卸载", func() {
				// TODO: 卸载逻辑
			}),
		)
	} else {
		actions = append(actions, widget.NewButton("安装", func() {
			// TODO: 安装逻辑
		}))
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

	// 右侧：展示从远端查询到的最新版本信息（查询失败时为失败原因）
	right := buildVersionPanel(rel, checkErr, st, onRetry)

	return container.NewBorder(nil, bottomBar(), left, nil, right)
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

// buildCheckFailedPanel 组装右侧信息面板的查询失败形态。
// 不整屏报错，只在原本展示版本信息的位置给出失败原因与重试入口，
// 使已安装用户在网络异常时依然可以使用「卸载」。
func buildCheckFailedPanel(checkErr error, onRetry func()) fyne.CanvasObject {
	title := widget.NewLabelWithStyle("无法获取线上版本", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})

	hint := widget.NewLabel("请检查网络连接后重试")
	hint.Alignment = fyne.TextAlignCenter
	hint.Wrapping = fyne.TextWrapWord

	// 透出真实失败原因
	detail := widget.NewLabel("原因：" + checkErr.Error())
	detail.Alignment = fyne.TextAlignCenter
	detail.Wrapping = fyne.TextWrapWord

	retry := widget.NewButton("重试", onRetry)

	return container.NewVBox(
		layout.NewSpacer(),
		title,
		hint,
		detail,
		container.NewCenter(retry),
		layout.NewSpacer(),
	)
}

func main() {
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

	var startVersionCheck func()

	showMain := func(rel *releaseInfo, checkErr error, st installState) {
		w.SetContent(buildMainUI(rel, checkErr, st, func() { startVersionCheck() }))
	}

	// 打开后：先显示转圈圈闪屏查询当前版本信息，
	// 完成后切到主界面展示最新版本；查询失败时同样进主界面，
	// 只在右侧面板展示失败原因与重试，不整屏报错。
	startVersionCheck = func() {
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
				showMain(rel, err, st)
			})
		}()
	}

	startVersionCheck()
	w.ShowAndRun()
}
