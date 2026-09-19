package uifx

import (
	"image/color"
	"math"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// spinnerDotCount 是旋转指示器上的圆点数量。
const spinnerDotCount = 8

// Spinner 是旋转点阵加载指示器：一圈圆点依次点亮，拖出渐隐的尾迹。
type Spinner struct {
	widget.BaseWidget
}

// NewSpinner 创建并启动一个旋转点阵指示器。
func NewSpinner() *Spinner {
	s := &Spinner{}
	s.ExtendBaseWidget(s)
	return s
}

func (s *Spinner) CreateRenderer() fyne.WidgetRenderer {
	r := &spinnerRenderer{base: toNRGBA(theme.Color(theme.ColorNamePrimary))}
	for i := 0; i < spinnerDotCount; i++ {
		dot := canvas.NewCircle(r.base)
		r.dots = append(r.dots, dot)
	}
	r.anim = &fyne.Animation{
		Duration:    1100 * time.Millisecond,
		RepeatCount: fyne.AnimationRepeatForever,
		Curve:       fyne.AnimationLinear,
		Tick:        r.animate,
	}
	if animationsOn() {
		r.anim.Start()
	} else {
		r.paint(0) // 关闭动画时给出静态的一圈暗点
	}
	return r
}

type spinnerRenderer struct {
	anim  *fyne.Animation
	dots  []*canvas.Circle
	base  color.NRGBA
	head  float32
	bound fyne.Size
}

func (r *spinnerRenderer) Destroy() { r.anim.Stop() }

func (r *spinnerRenderer) Layout(size fyne.Size) {
	r.bound = size
	diameter := fyne.Min(size.Width, size.Height)
	ringR := diameter / 2 * 0.78
	dotD := diameter / 7
	cx, cy := size.Width/2, size.Height/2
	for i, dot := range r.dots {
		ang := -math.Pi/2 + float64(i)*2*math.Pi/spinnerDotCount
		x := cx + ringR*float32(math.Cos(ang))
		y := cy + ringR*float32(math.Sin(ang))
		dot.Move(fyne.NewPos(x-dotD/2, y-dotD/2))
		dot.Resize(fyne.NewSquareSize(dotD))
		dot.Refresh()
	}
}

func (r *spinnerRenderer) MinSize() fyne.Size { return fyne.NewSquareSize(28) }

func (r *spinnerRenderer) Objects() []fyne.CanvasObject {
	objs := make([]fyne.CanvasObject, len(r.dots))
	for i, d := range r.dots {
		objs[i] = d
	}
	return objs
}

func (r *spinnerRenderer) Refresh() {
	r.base = toNRGBA(theme.Color(theme.ColorNamePrimary))
	r.paint(r.head)
}

// animate 每帧推进头部位置，头部最亮、越靠后的尾迹越暗。
func (r *spinnerRenderer) animate(f float32) { r.paint(f * spinnerDotCount) }

// paint 按头部位置（圆点序号，可为小数）给每个圆点上色。
func (r *spinnerRenderer) paint(head float32) {
	r.head = head
	for i, dot := range r.dots {
		d := float32(math.Mod(float64(head-float32(i)), spinnerDotCount))
		if d < 0 {
			d += spinnerDotCount
		}
		a := uint8(0x24 + float32(0xff-0x24)*(1-d/spinnerDotCount))
		dot.FillColor = fadeAlpha(r.base, a)
		dot.Refresh()
	}
}
