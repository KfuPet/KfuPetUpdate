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
// SetValue 的变化以短补间动画过渡，进度条平滑推进而不是按汇报粒度跳变。
type ShineBar struct {
	widget.BaseWidget
	bar    *widget.ProgressBar
	cur    float64 // 当前显示值（动画推进中）
	target float64
	anim   *fyne.Animation
}

// NewShineBar 创建带流光的进度条。
func NewShineBar() *ShineBar {
	s := &ShineBar{bar: widget.NewProgressBar()}
	s.ExtendBaseWidget(s)
	return s
}

// SetMax 设置进度条最大值。
func (s *ShineBar) SetMax(v float64) { s.bar.Max = v }

// SetValue 设置目标进度；从当前显示值补间过去。
// 进度回退（重试）或系统关闭动画时直接到位。
func (s *ShineBar) SetValue(v float64) {
	if !animationsOn() || v <= s.cur {
		s.stopAnim()
		s.cur = v
		s.bar.SetValue(v)
		return
	}
	s.stopAnim()
	from := s.cur
	s.target = v
	s.anim = &fyne.Animation{
		Duration: 240 * time.Millisecond,
		Curve:    fyne.AnimationLinear, // 缓动在 easeOutCubic 里做
		Tick: func(f float32) {
			s.cur = from + (s.target-from)*float64(easeOutCubic(f))
			s.bar.SetValue(s.cur)
		},
	}
	s.anim.Start()
}

// stopAnim 中断进行中的补间；当前显示值保留在已推进到的位置，
// 下一次 SetValue 从这里接着走，不会回跳。
func (s *ShineBar) stopAnim() {
	if s.anim == nil {
		return
	}
	s.anim.Stop()
	s.anim = nil
}

func (s *ShineBar) CreateRenderer() fyne.WidgetRenderer {
	// 光带整条轨道扫过：实条上是明显的白光，空轨道上换成很淡的一层，
	// 保留完整的扫过感，又不会像多出一根进度条。
	shine := canvas.NewRectangle(color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 96})
	faint := canvas.NewRectangle(color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 28})
	r := &shineBarRenderer{bar: s.bar, shine: shine, faint: faint}
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
		faint.Hide()
	}
	return r
}

type shineBarRenderer struct {
	bar   *widget.ProgressBar
	shine *canvas.Rectangle // 落在实条上的高光
	faint *canvas.Rectangle // 扫过空轨道时的淡光
	anim  *fyne.Animation
	bound fyne.Size
	phase float32 // 当前扫光相位，布局重排时沿用，避免高光被重置
}

func (r *shineBarRenderer) Destroy() { r.anim.Stop() }

func (r *shineBarRenderer) Layout(size fyne.Size) {
	r.bound = size
	r.bar.Move(fyne.NewPos(0, 0))
	r.bar.Resize(size)
	// 布局会被频繁触发，这里必须沿用当前相位重新计算几何；
	// 若重置为 0，高光会在每次布局时被隐藏，画面上就永远看不到。
	r.animate(r.phase)
}

func (r *shineBarRenderer) MinSize() fyne.Size { return r.bar.MinSize() }

func (r *shineBarRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.bar, r.faint, r.shine}
}

func (r *shineBarRenderer) Refresh() {}

// animate 让光带从左到右扫过整条轨道，扫出右缘后回到左侧重来。
// 光带在空轨道上是很淡的一层，进入已填充实条后变成明显白光，
// 既保留完整的扫过感，又不会在空轨道上像多出一根进度条。
func (r *shineBarRenderer) animate(f float32) {
	r.phase = f
	w, h := r.bound.Width, r.bound.Height
	if w <= 0 {
		return
	}
	sw := w * 0.22
	x := -sw + (w+2*sw)*f

	// 光带整条轨道可见，越出轨道两端的部分直接裁掉。
	left := x
	if left < 0 {
		left = 0
	}
	right := x + sw
	if right > w {
		right = w
	}
	if right <= left {
		r.shine.Hide()
		r.faint.Hide()
		return
	}
	r.faint.Show()
	r.faint.Move(fyne.NewPos(left, 1))
	r.faint.Resize(fyne.NewSize(right-left, h-2))
	r.faint.Refresh()

	// 与实条重叠的部分改用更亮的白光，扫过进度头部后回到淡光。
	filled := float32(0)
	if r.bar.Max > 0 {
		filled = clamp01(float32(r.bar.Value / r.bar.Max))
	}
	strongRight := w * filled
	if strongRight > right {
		strongRight = right
	}
	if strongRight <= left {
		r.shine.Hide()
		return
	}
	r.shine.Show()
	r.shine.Move(fyne.NewPos(left, 1))
	r.shine.Resize(fyne.NewSize(strongRight-left, h-2))
	r.shine.Refresh()
}
