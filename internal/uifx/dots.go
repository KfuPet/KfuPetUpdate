package uifx

import (
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// dotsCount 是跳动省略号的圆点数量。
const dotsCount = 3

// Dots 是三个依次跳动的圆点，跟在"正在查询"这类文案后面充当会动的省略号。
type Dots struct {
	widget.BaseWidget
}

// NewDots 创建并启动跳动省略号。
func NewDots() *Dots {
	d := &Dots{}
	d.ExtendBaseWidget(d)
	return d
}

func (d *Dots) CreateRenderer() fyne.WidgetRenderer {
	r := &dotsRenderer{base: toNRGBA(theme.Color(theme.ColorNameForeground))}
	for i := 0; i < dotsCount; i++ {
		r.dots = append(r.dots, canvas.NewCircle(r.base))
	}
	r.anim = &fyne.Animation{
		Duration:    1200 * time.Millisecond,
		RepeatCount: fyne.AnimationRepeatForever,
		Curve:       fyne.AnimationLinear,
		Tick:        r.animate,
	}
	if animationsOn() {
		r.anim.Start()
	} else {
		r.animate(0)
	}
	return r
}

type dotsRenderer struct {
	anim  *fyne.Animation
	dots  []*canvas.Circle
	base  color.NRGBA
	yoff  [dotsCount]float32
	alph  [dotsCount]uint8
	bound fyne.Size
}

func (r *dotsRenderer) Destroy() { r.anim.Stop() }

func (r *dotsRenderer) Layout(size fyne.Size) {
	r.bound = size
	r.place()
}

func (r *dotsRenderer) MinSize() fyne.Size { return fyne.NewSize(26, 10) }

func (r *dotsRenderer) Objects() []fyne.CanvasObject {
	objs := make([]fyne.CanvasObject, len(r.dots))
	for i, d := range r.dots {
		objs[i] = d
	}
	return objs
}

func (r *dotsRenderer) Refresh() {
	r.base = toNRGBA(theme.Color(theme.ColorNameForeground))
	r.place()
}

// animate 让每个点依次抬高再落下，同时透明度跟着跳动。
func (r *dotsRenderer) animate(f float32) {
	amp := fyne.Max(r.bound.Height*0.30, 2.5)
	for i := range r.dots {
		ph := f - float32(i)*0.18
		if ph < 0 {
			ph += 1
		}
		b := sine01(ph*2 - 0.25) // 前半周期抬起，后半周期落下
		if ph > 0.5 {
			b = 0
		}
		r.yoff[i] = -amp * b
		r.alph[i] = uint8(0x55 + 0xaa*b)
	}
	r.place()
}

// place 按当前跳动偏移摆放圆点。
func (r *dotsRenderer) place() {
	if r.bound.Width <= 0 {
		return
	}
	gap := r.bound.Width / dotsCount
	d := fyne.Min(r.bound.Height*0.42, gap*0.6)
	for i, dot := range r.dots {
		x := gap*float32(i) + gap/2
		cy := r.bound.Height / 2
		dot.Move(fyne.NewPos(x-d/2, cy-d/2+r.yoff[i]))
		dot.Resize(fyne.NewSquareSize(d))
		a := r.alph[i]
		if a == 0 {
			a = 0x55
		}
		dot.FillColor = fadeAlpha(r.base, a)
		dot.Refresh()
	}
}
