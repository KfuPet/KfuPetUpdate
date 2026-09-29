package uifx

import (
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
)

// TextPulse 给 canvas.Text 的内容切换提供「淡出旧文案 → 替换 → 淡入新文案」
// 的脉冲过渡，避免阶段标题这类短文案在原地生硬地跳变。
type TextPulse struct {
	text *canvas.Text
	anim *fyne.Animation
	// OnSwap 在文案被替换（或动画关闭时立即替换）后调用，
	// 比如让父容器按新文案长度重新居中。
	OnSwap func()
}

// NewTextPulse 包装一段文本；之后改用 Set 更新文案以获得过渡动效。
func NewTextPulse(t *canvas.Text) *TextPulse {
	return &TextPulse{text: t}
}

// Set 更新文案。与当前文案相同（如高频进度汇报）时不动，避免反复播放。
func (p *TextPulse) Set(s string, d time.Duration) {
	if p.text.Text == s {
		return
	}
	p.stop()

	if !animationsOn() {
		p.text.Text = s
		p.text.Refresh()
		if p.OnSwap != nil {
			p.OnSwap()
		}
		return
	}

	base := toNRGBA(p.text.Color)
	p.anim = &fyne.Animation{
		Duration: d,
		Curve:    fyne.AnimationLinear, // 缓动在 easeOutCubic 里做
		Tick: func(f float32) {
			var a float32
			if f < 0.5 {
				a = 1 - easeOutCubic(f*2)
			} else {
				// 中点换文案：只换一次
				if p.text.Text != s {
					p.text.Text = s
					if p.OnSwap != nil {
						onSwap := p.OnSwap
						// tick 在渲染循环里执行，布局类回调挪到主循环稍后处理
						fyne.Do(onSwap)
					}
				}
				a = easeOutCubic((f - 0.5) * 2)
			}
			p.text.Color = fadeAlpha(base, uint8(0xff*clamp01(a)))
			p.text.Refresh()
		},
	}
	p.anim.Start()
}

// stop 中断进行中的过渡，并把颜色归位（动画被截断时文案不能停在半透明）。
func (p *TextPulse) stop() {
	if p.anim == nil {
		return
	}
	p.anim.Stop()
	p.anim = nil
	p.text.Color = fadeAlpha(toNRGBA(p.text.Color), 0xff)
	p.text.Refresh()
}
