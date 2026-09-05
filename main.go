package main

import (
	"context"
	"image/color"
	"net/url"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type vfillLayout struct {
	width      float32 // 左侧栏整体宽度
	itemHeight float32 // 每个按钮的高度
	spacing    float32 // 按钮之间的间距
}

func (l *vfillLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	n := float32(len(objects))
	total := n*l.itemHeight + (n-1)*l.spacing
	bottomMargin := float32(24)
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

// linkText 是一个无内边距的白色下划线文字，点击后打开指定链接。
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

func buildVersionPanel(rel *releaseInfo) fyne.CanvasObject {
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
	return container.NewCenter(container.NewVBox(rows...))
}

// publishLine 生成"发布于 yyyy-MM-dd"文本；时间为零值时提示未知。
func publishLine(rel *releaseInfo) string {
	if rel.PublishedAt.IsZero() {
		return "发布日期未知"
	}
	return "发布于 " + rel.PublishedAt.Local().Format("2006-01-02")
}

// buildMainUI 组装主界面：左侧操作按钮 + 右侧最新版本信息 + 底部版权。
func buildMainUI(rel *releaseInfo) fyne.CanvasObject {
	installBtn := widget.NewButton("安装", func() {
		// TODO: 安装逻辑
	})
	upgradeBtn := widget.NewButton("升级", func() {
		// TODO: 升级逻辑
	})
	uninstallBtn := widget.NewButton("卸载", func() {
		// TODO: 卸载逻辑
	})

	// 左侧：三个长条形按钮，横向填满、垂直居中
	left := container.New(&vfillLayout{
		width:      180,
		itemHeight: 48,
		spacing:    20,
	}, installBtn, upgradeBtn, uninstallBtn)

	// 右侧：展示从远端查询到的最新版本信息
	right := buildVersionPanel(rel)

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

// failedView 组装查询失败的闪屏：提示 + 重试按钮。
func failedView(onRetry func()) fyne.CanvasObject {
	title := widget.NewLabelWithStyle("无法获取当前版本信息", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	hint := smallText("请检查网络连接后重试")
	retry := widget.NewButton("重试", onRetry)

	return container.NewCenter(container.NewVBox(
		container.NewCenter(title),
		container.NewCenter(hint),
		container.NewCenter(retry),
	))
}

func main() {
	a := app.New()
	w := a.NewWindow("KfuPetUpdate")
	// 长方形界面
	w.Resize(fyne.NewSize(600, 360))
	w.SetFixedSize(true)
	w.CenterOnScreen()

	checker := newUpdateChecker()

	showMain := func(rel *releaseInfo) {
		w.SetContent(buildMainUI(rel))
	}

	var startVersionCheck func()
	showFailed := func() {
		w.SetContent(failedView(func() { startVersionCheck() }))
	}

	// 打开后：先显示转圈圈闪屏查询当前版本信息，
	// 完成后切到主界面展示最新版本；全部源不可用时展示重试。
	startVersionCheck = func() {
		w.SetContent(checkingView())
		shownAt := time.Now() // 记录闪屏开始时刻，用于保证最短展示时长
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
			defer cancel()

			rel, err := checker.check(ctx)

			// 查询提前完成时，补足剩余时长再切换，避免闪屏一闪而过
			if remain := minSplashDuration - time.Since(shownAt); remain > 0 {
				time.Sleep(remain)
			}

			fyne.Do(func() {
				if err != nil {
					showFailed()
					return
				}
				showMain(rel)
			})
		}()
	}

	startVersionCheck()
	w.ShowAndRun()
}
