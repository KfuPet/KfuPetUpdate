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

// ResultMark 是安装结果标记：成功时圆圈带弹性弹出、对勾逐笔描出；
// 失败时画红叉，描完后左右抖动几下以示强调。
type ResultMark struct {
	widget.BaseWidget
	ok bool
}

// NewResultMark 创建结果标记，ok 为真画对勾，为假画叉。
func NewResultMark(ok bool) *ResultMark {
	m := &ResultMark{ok: ok}
	m.ExtendBaseWidget(m)
	return m
}

func (m *ResultMark) CreateRenderer() fyne.WidgetRenderer {
	name := theme.ColorNameSuccess
	if !m.ok {
		name = theme.ColorNameError
	}
	col := toNRGBA(theme.Color(name))

	circle := canvas.NewCircle(color.Transparent)
	circle.StrokeWidth = 4
	r := &markRenderer{owner: m, circle: circle, col: col}
	r.l1 = &canvas.Line{StrokeColor: col, StrokeWidth: 4}
	r.l2 = &canvas.Line{StrokeColor: col, StrokeWidth: 4}

	r.anim = &fyne.Animation{
		Duration: 650 * time.Millisecond,
		Curve:    fyne.AnimationLinear, // 各阶段在 place 内自行缓动
		Tick: func(f float32) {
			r.progress = f
			r.place()
			if f >= 1 && !m.ok {
				r.startShake()
			}
		},
	}
	if animationsOn() {
		r.anim.Start()
	} else {
		r.progress = 1
	}
	return r
}

type markRenderer struct {
	owner    *ResultMark
	circle   *canvas.Circle
	l1       *canvas.Line
	l2       *canvas.Line
	anim     *fyne.Animation
	shake    *fyne.Animation
	col      color.NRGBA
	progress float32
	shakeX   float32
	shaking  bool
	bound    fyne.Size
}

func (r *markRenderer) Destroy() {
	r.anim.Stop()
	if r.shake != nil {
		r.shake.Stop()
	}
}

func (r *markRenderer) Layout(size fyne.Size) {
	r.bound = size
	r.place()
}

func (r *markRenderer) MinSize() fyne.Size { return fyne.NewSquareSize(56) }

func (r *markRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.circle, r.l1, r.l2}
}

func (r *markRenderer) Refresh() { r.place() }

// startShake 画完叉之后左右快速抖动，振幅逐渐衰减。
func (r *markRenderer) startShake() {
	if r.shaking {
		return
	}
	r.shaking = true
	r.shake = &fyne.Animation{
		Duration: 420 * time.Millisecond,
		Curve:    fyne.AnimationLinear,
		Tick: func(f float32) {
			r.shakeX = 5 * float32(math.Sin(float64(f)*3*2*math.Pi)) * (1 - f)
			r.place()
		},
	}
	r.shake.Start()
}

// place 按当前绘制进度与抖动偏移摆放圆圈和两笔。
// 进度划分：0~45% 圆圈弹性弹出并淡入；45%~70% 描第一笔；70%~100% 描第二笔。
func (r *markRenderer) place() {
	s := fyne.Min(r.bound.Width, r.bound.Height)
	if s <= 0 {
		return
	}
	ox := (r.bound.Width-s)/2 + r.shakeX
	oy := (r.bound.Height - s) / 2

	const inset = float32(4)
	pop := float32(1)
	if r.progress < 0.45 {
		pop = 0.55 + 0.45*easeOutBack(r.progress/0.45)
	}
	dia := (s - 2*inset) * pop
	r.circle.Move(fyne.NewPos(ox+inset+((s-2*inset)-dia)/2, oy+inset+((s-2*inset)-dia)/2))
	r.circle.Resize(fyne.NewSquareSize(dia))
	r.circle.StrokeColor = fadeAlpha(r.col, uint8(0xff*clamp01(r.progress/0.3)))
	r.circle.Refresh()

	t1 := clamp01((r.progress - 0.45) / 0.25)
	t2 := clamp01((r.progress - 0.70) / 0.30)
	if r.owner.ok {
		p0 := fyne.NewPos(ox+0.27*s, oy+0.53*s)
		p1 := fyne.NewPos(ox+0.44*s, oy+0.70*s)
		p2 := fyne.NewPos(ox+0.75*s, oy+0.32*s)
		drawStroke(r.l1, p0, p1, t1, r.col)
		drawStroke(r.l2, p1, p2, t2, r.col)
	} else {
		a0 := fyne.NewPos(ox+0.30*s, oy+0.30*s) // 第一笔：左上 → 右下
		a1 := fyne.NewPos(ox+0.70*s, oy+0.70*s)
		b0 := fyne.NewPos(ox+0.70*s, oy+0.30*s) // 第二笔：右上 → 左下
		b1 := fyne.NewPos(ox+0.30*s, oy+0.70*s)
		drawStroke(r.l1, a0, a1, t1, r.col)
		drawStroke(r.l2, b0, b1, t2, r.col)
	}
}
