package uifx

import (
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
)

// slideFadeDistance 是翻页入场时内容上滑的距离（像素）。
const slideFadeDistance = float32(18)

// slideLayout 把唯一子元素按 dy 竖直偏移摆放（尺寸不变），
// 供 SlideFadeIn 在入场动画里把内容从下方托上来。
type slideLayout struct {
	dy float32
}

func (l *slideLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objs {
		o.Move(fyne.NewPos(0, l.dy))
		o.Resize(size)
	}
}

func (l *slideLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	s := fyne.NewSize(0, 0)
	for _, o := range objs {
		s = s.Max(o.MinSize())
	}
	return s
}

// SlideFadeIn 是整页入场动效：内容从下方 18px 处滑上来，
// 同时叠一层与背景同色的遮罩淡出，形成「上滑 + 淡入」。
// 遮罩只是 canvas.Rectangle，不响应事件，不会挡住后续点击。
func SlideFadeIn(content fyne.CanvasObject, d time.Duration) fyne.CanvasObject {
	base := toNRGBA(theme.Color(theme.ColorNameBackground))
	overlay := canvas.NewRectangle(fadeAlpha(base, 0))
	lay := &slideLayout{}
	wrapped := container.New(lay, content)

	if animationsOn() {
		lay.dy = slideFadeDistance
		overlay.FillColor = fadeAlpha(base, 0xff)
		anim := fyne.NewAnimation(d, func(f float32) {
			e := easeOutCubic(f)
			lay.dy = slideFadeDistance * (1 - e)
			content.Move(fyne.NewPos(0, lay.dy))
			overlay.FillColor = fadeAlpha(base, uint8(0xff*(1-e)))
			canvas.Refresh(overlay)
		})
		anim.Curve = fyne.AnimationLinear
		anim.Start()
	}

	return container.NewStack(wrapped, overlay)
}
