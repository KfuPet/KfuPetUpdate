package uifx

import (
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
)

// Cover 是叠在内容之上、与背景同色的一层遮罩，用来让该块内容单独渐隐/渐显。
// 遮罩尺寸随内容走，且不响应事件，不会挡住内容的点击。
type Cover struct {
	rect *canvas.Rectangle
}

// NewCover 把内容包一层遮罩，返回容器与遮罩控制器。
// 遮罩初始完全透明，内容正常可见。
func NewCover(content fyne.CanvasObject) (fyne.CanvasObject, *Cover) {
	cover := &Cover{rect: canvas.NewRectangle(fadeAlpha(toNRGBA(theme.Color(theme.ColorNameBackground)), 0))}
	return container.NewStack(content, cover.rect), cover
}

// Conceal 立即遮住内容（遮罩变为不透明），不播放动画。
// 用于在两个页面的渐隐/渐显之间搭桥：先遮住，切页后再 FadeIn。
func (c *Cover) Conceal() { c.paint(1) }

// FadeOut 播放"内容渐隐"：遮罩由透明变为不透明，结束后调用 onDone（可为 nil）。
func (c *Cover) FadeOut(d time.Duration, onDone func()) { c.run(0, 1, d, onDone) }

// FadeIn 播放"内容渐显"：遮罩由不透明变为透明。
func (c *Cover) FadeIn(d time.Duration) { c.run(1, 0, d, nil) }

// run 在 from→to 的不透明度之间补间；系统关闭动画时直接落到终态。
func (c *Cover) run(from, to float32, d time.Duration, onDone func()) {
	c.paint(from)
	if !animationsOn() {
		c.paint(to)
		if onDone != nil {
			fyne.Do(onDone)
		}
		return
	}

	anim := &fyne.Animation{
		Duration: d,
		Curve:    fyne.AnimationLinear, // 缓动在 easeOutCubic 里做
		Tick: func(f float32) {
			c.paint(from + (to-from)*easeOutCubic(f))
			if f >= 1 && onDone != nil {
				// 动画 tick 在渲染循环里执行，回调挪到主循环稍后处理，
				// 避免正在补间时就去改窗口内容。
				fyne.Do(onDone)
			}
		},
	}
	anim.Start()
}

// paint 把遮罩设为指定不透明度的背景色。
func (c *Cover) paint(alpha float32) {
	c.rect.FillColor = fadeAlpha(toNRGBA(theme.Color(theme.ColorNameBackground)), uint8(0xff*clamp01(alpha)))
	c.rect.Refresh()
}
