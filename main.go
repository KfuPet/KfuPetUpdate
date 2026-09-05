package main

import (
	"image/color"
	"net/url"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// vfillLayout 让子控件在垂直方向按固定高度排列，并横向填满可用宽度。
// 用于把左侧的三个按钮做成等宽、长条形的样式。
type vfillLayout struct {
	width      float32 // 左侧栏整体宽度
	itemHeight float32 // 每个按钮的高度
	spacing    float32 // 按钮之间的间距
}

func (l *vfillLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	n := float32(len(objects))
	total := n*l.itemHeight + (n-1)*l.spacing
	// 按钮组整体靠下，最下面的按钮距底部留一点间距
	bottomMargin := float32(24)
	y := size.Height - total - bottomMargin
	// 左侧留出空隙，让按钮整体稍微右移，不贴窗口左边缘
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

func main() {
	a := app.New()
	w := a.NewWindow("KfuPetUpdate")
	// 长方形界面
	w.Resize(fyne.NewSize(600, 360))
	w.SetFixedSize(true)
	w.CenterOnScreen()

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

	// 右侧：预留区域，后续放宣传图、图标等
	right := widget.NewLabel("宣传图区域（预留）")

	// 底部：分隔线 + 版权信息（KfuPet 可点击跳转）
	kfupetURL, _ := url.Parse("https://github.com/KfuPet")
	copyrightText := canvas.NewText("Copyright © 2025 - 2026 ", color.Gray{Y: 0x99})
	copyrightText.TextSize = theme.CaptionTextSize()
	kfupetLink := newLinkText("KfuPet", kfupetURL)
	copyrightLine := container.NewHBox(copyrightText, kfupetLink)
	bottom := container.NewBorder(
		widget.NewSeparator(),              // 顶部细横线，横贯窗口宽度
		nil,                                // 底部
		nil,                                // 左侧
		nil,                                // 右侧
		container.NewCenter(copyrightLine), // 版权信息居中
	)

	content := container.NewBorder(nil, bottom, left, nil, right)

	w.SetContent(content)

	w.ShowAndRun()
}
