// Package uifx 是界面动效小组件库：加载指示、步骤清单、结果标记、
// 彩带、淡入切换等。所有控件在系统关闭动画（ShowAnimations 为假）时
// 都退回静态形态，不强行播放。
package uifx

import (
	"image/color"
	"math"

	"fyne.io/fyne/v2"
)

// easeOutCubic 先快后慢的缓出曲线，用于淡入等入场动画。
func easeOutCubic(t float32) float32 {
	t = clamp01(t)
	u := 1 - t
	return 1 - u*u*u
}

// easeOutBack 带轻微过冲回弹的缓出曲线，用于打勾、弹出等强调动画。
func easeOutBack(t float32) float32 {
	t = clamp01(t)
	const c = 1.70158
	u := t - 1
	return 1 + (c+1)*u*u*u + c*u*u
}

// sine01 返回 0..1 之间平滑往返的正弦值，phase 每 +1 走完一个完整周期。
func sine01(phase float32) float32 {
	return 0.5 + 0.5*float32(math.Sin(float64(phase)*2*math.Pi))
}

// clamp01 把值夹到 [0,1]。
func clamp01(v float32) float32 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// toNRGBA 把任意颜色转成 NRGBA，便于逐帧改写透明度。
func toNRGBA(c color.Color) color.NRGBA {
	r, g, b, a := c.RGBA()
	return color.NRGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)}
}

// fadeAlpha 返回只替换透明通道后的颜色。
func fadeAlpha(c color.NRGBA, a uint8) color.NRGBA {
	return color.NRGBA{R: c.R, G: c.G, B: c.B, A: a}
}

// animationsOn 报告当前系统设置是否允许播放动画。
func animationsOn() bool {
	return fyne.CurrentApp().Settings().ShowAnimations()
}
