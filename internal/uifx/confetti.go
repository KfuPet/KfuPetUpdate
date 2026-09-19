package uifx

import (
	"image/color"
	"math"
	"math/rand/v2"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// confettiCount 是彩带粒子的数量（固定对象池，避免播放中增删渲染对象）。
const confettiCount = 90

// confettiGravity 是彩纸下坠的重力加速度（像素/秒²）。
const confettiGravity = 520

// confettiParticle 是一片彩纸：起点按比例存储，每帧换算成绝对坐标，
// 因此控件尚未排版完成时开始播放也不会偏位。
type confettiParticle struct {
	rect        *canvas.Rectangle
	base        color.NRGBA
	x0, y0      float32 // 起点（占控件宽高的比例）
	vx, vy      float32 // 初速度（像素/秒）
	amp         float32 // 左右飘摆幅度（像素）
	freq, phase float32 // 飘摆频率（赫兹）与相位
	pw, ph      float32 // 彩纸尺寸
}

// Confetti 是庆祝彩带：在控件上半部炸开一片彩纸，受重力下落并左右飘摆，
// 末尾渐隐。盖在完成页内容之上即可（彩纸不响应事件，不挡点击）。
type Confetti struct {
	widget.BaseWidget
}

// NewConfetti 创建彩带层；控件显示时自动炸开一次。
func NewConfetti() *Confetti {
	c := &Confetti{}
	c.ExtendBaseWidget(c)
	return c
}

func (c *Confetti) CreateRenderer() fyne.WidgetRenderer {
	r := &confettiRenderer{}
	palette := []color.NRGBA{
		toNRGBA(theme.Color(theme.ColorNamePrimary)),
		toNRGBA(theme.Color(theme.ColorNameSuccess)),
		toNRGBA(theme.Color(theme.ColorNameWarning)),
		toNRGBA(theme.Color(theme.ColorNameError)),
		{R: 0xff, G: 0x6b, B: 0xb1, A: 0xff}, // 粉
		{R: 0xff, G: 0xc1, B: 0x07, A: 0xff}, // 金
		{R: 0x39, G: 0xc5, B: 0xcf, A: 0xff}, // 青
	}
	for i := 0; i < confettiCount; i++ {
		rect := canvas.NewRectangle(color.Transparent)
		rect.Hide()
		p := &confettiParticle{rect: rect, base: palette[rand.IntN(len(palette))]}
		r.particles = append(r.particles, p)
		r.objs = append(r.objs, rect)
	}
	r.anim = &fyne.Animation{
		Duration: 3400 * time.Millisecond,
		Curve:    fyne.AnimationLinear,
		Tick:     r.tick,
	}
	if animationsOn() {
		r.burst()
		r.anim.Start()
	}
	return r
}

type confettiRenderer struct {
	anim      *fyne.Animation
	particles []*confettiParticle
	objs      []fyne.CanvasObject
	bound     fyne.Size
}

func (r *confettiRenderer) Destroy() { r.anim.Stop() }

func (r *confettiRenderer) Layout(size fyne.Size) { r.bound = size }

func (r *confettiRenderer) MinSize() fyne.Size { return fyne.NewSize(1, 1) }

func (r *confettiRenderer) Objects() []fyne.CanvasObject { return r.objs }

func (r *confettiRenderer) Refresh() {}

// burst 给每片彩纸赋予随机起点、速度与飘摆参数。
func (r *confettiRenderer) burst() {
	for _, p := range r.particles {
		p.x0 = 0.35 + 0.30*rand.Float32()
		p.y0 = 0.16 + 0.12*rand.Float32()
		p.vx = (rand.Float32()*2 - 1) * 150
		p.vy = -(150 + 180*rand.Float32())
		p.amp = 6 + 22*rand.Float32()
		p.freq = 1 + 2*rand.Float32()
		p.phase = rand.Float32()
		p.pw = 4 + 3*rand.Float32()
		p.ph = 6 + 5*rand.Float32()
	}
}

// tick 推进彩纸位置：水平匀速 + 飘摆，垂直受重力加速，末段渐隐。
func (r *confettiRenderer) tick(f float32) {
	if r.bound.Width < 10 {
		return // 尚未排版，等下一帧
	}
	t := f * 3.4 // 播放时长（秒），与动画时长对应
	fade := 1 - clamp01((f-0.55)/0.45)
	for _, p := range r.particles {
		x := p.x0*r.bound.Width + p.vx*t + p.amp*float32(math.Sin(2*math.Pi*float64(p.freq*t+p.phase)))
		y := p.y0*r.bound.Height + p.vy*t + 0.5*confettiGravity*t*t
		if y > r.bound.Height+16 {
			p.rect.Hide()
			continue
		}
		p.rect.Show()
		p.rect.Move(fyne.NewPos(x, y))
		p.rect.Resize(fyne.NewSize(p.pw, p.ph))
		p.rect.FillColor = fadeAlpha(p.base, uint8(0xff*fade))
		p.rect.Refresh()
	}
}
