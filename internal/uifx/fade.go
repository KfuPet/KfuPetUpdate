package uifx

import (
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
)

// FadeIn 给内容叠一层与背景同色的遮罩，再把遮罩淡出，形成整页淡入。
// 遮罩只是 canvas.Rectangle，不响应事件，不会挡住后续点击。
func FadeIn(content fyne.CanvasObject, d time.Duration) fyne.CanvasObject {
	base := toNRGBA(theme.Color(theme.ColorNameBackground))
	overlay := canvas.NewRectangle(fadeAlpha(base, 0))

	if animationsOn() {
		overlay.FillColor = fadeAlpha(base, 0xff)
		anim := fyne.NewAnimation(d, func(f float32) {
			overlay.FillColor = fadeAlpha(base, uint8(0xff*(1-easeOutCubic(f))))
			canvas.Refresh(overlay)
		})
		anim.Curve = fyne.AnimationLinear
		anim.Start()
	}

	return container.NewStack(content, overlay)
}
