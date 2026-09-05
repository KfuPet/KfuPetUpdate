package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
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
	// 按钮组在左侧垂直居中
	y := (size.Height - total) / 2
	for _, o := range objects {
		o.Resize(fyne.NewSize(size.Width, l.itemHeight))
		o.Move(fyne.NewPos(0, y))
		y += l.itemHeight + l.spacing
	}
}

func (l *vfillLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	n := float32(len(objects))
	h := n*l.itemHeight + (n-1)*l.spacing
	return fyne.NewSize(l.width, h)
}

func main() {
	a := app.New()
	w := a.NewWindow("KfuPet 安装 / 升级 / 卸载工具")
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

	content := container.NewBorder(nil, nil, left, nil, right)

	w.SetContent(content)

	w.ShowAndRun()
}
