package uifx

import (
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
)

// ShineBar 在确定进度条上叠加一条循环扫过的高光，
// 让"有明确进度"的同时也有"正在动"的感觉。
type ShineBar struct {
	widget.BaseWidget
	bar *widget.ProgressBar
}

// NewShineBar 创建带流光的进度条。
func NewShineBar() *ShineBar {
	s := &ShineBar{bar: widget.NewProgressBar()}
	s.ExtendBaseWidget(s)
	return s
}

// SetMax 设置进度条最大值。
func (s *ShineBar) SetMax(v float64) { s.bar.Max = v }

// SetValue 设置当前进度。
func (s *ShineBar) SetValue(v float64) { s.bar.SetValue(v) }

func (s *ShineBar) CreateRenderer() fyne.WidgetRenderer {
	shine := canvas.NewRectangle(color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 38})
	r := &shineBarRenderer{bar: s.bar, shine: shine}
	r.anim = &fyne.Animation{
		Duration:    1400 * time.Millisecond,
		RepeatCount: fyne.AnimationRepeatForever,
		Curve:       fyne.AnimationLinear,
		Tick:        r.animate,
	}
	if animationsOn() {
		r.anim.Start()
	} else {
		shine.Hide()
	}
	return r
}

type shineBarRenderer struct {
	bar   *widget.ProgressBar
	shine *canvas.Rectangle
	anim  *fyne.Animation
	bound fyne.Size
}

func (r *shineBarRenderer) Destroy() { r.anim.Stop() }

func (r *shineBarRenderer) Layout(size fyne.Size) {
	r.bound = size
	r.bar.Move(fyne.NewPos(0, 0))
	r.bar.Resize(size)
	r.animate(0)
}

func (r *shineBarRenderer) MinSize() fyne.Size { return r.bar.MinSize() }

func (r *shineBarRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.bar, r.shine}
}

func (r *shineBarRenderer) Refresh() {}

// animate 让高光条从左到右扫过，扫出右缘后回到左侧重来。
func (r *shineBarRenderer) animate(f float32) {
	w, h := r.bound.Width, r.bound.Height
	if w <= 0 {
		return
	}
	sw := w * 0.22
	x := -sw + (w+2*sw)*f
	r.shine.Move(fyne.NewPos(x, 1))
	r.shine.Resize(fyne.NewSize(sw, h-2))
	r.shine.Refresh()
}
