package uifx

import (
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// 步骤行的几何参数。
const (
	stepRowHeight = float32(26) // 每行高度
	stepRowGap    = float32(6)  // 行间距
	stepIconSize  = float32(16) // 状态图标（圈/点/勾）边长
	stepLabelX    = float32(28) // 文案起始 x
)

// stepRow 是一行步骤的全部画布对象。
type stepRow struct {
	ring  *canvas.Circle // 未开始：灰色空心圈
	dot   *canvas.Circle // 当前步：主题色实心点（脉冲）
	tick1 *canvas.Line   // 已完成：对勾第一笔
	tick2 *canvas.Line   // 已完成：对勾第二笔
	label *canvas.Text
	bx    float32 // 行内图标盒的左上角（Layout 写入，画勾动画读取）
	by    float32
}

// StepList 是安装步骤清单：已完成画绿色对勾（逐笔描出），
// 当前步骤圆点脉冲高亮，未开始步骤显示弱化的空心圈。
type StepList struct {
	widget.BaseWidget
	stages  []string
	current int
}

// NewStepList 创建步骤清单，stages 为展示文案（调用方自行编号）。
func NewStepList(stages []string) *StepList {
	s := &StepList{stages: stages}
	s.ExtendBaseWidget(s)
	return s
}

// SetCurrent 把当前步骤推进到 idx；新完成的行会播放画勾动画。
func (s *StepList) SetCurrent(idx int) {
	if idx == s.current {
		return
	}
	s.current = idx
	s.Refresh()
}

func (s *StepList) CreateRenderer() fyne.WidgetRenderer {
	fg := toNRGBA(theme.Color(theme.ColorNameForeground))
	primary := toNRGBA(theme.Color(theme.ColorNamePrimary))
	success := toNRGBA(theme.Color(theme.ColorNameSuccess))

	r := &stepListRenderer{owner: s, fg: fg, primary: primary, success: success, ticked: map[int]bool{}}
	for _, name := range s.stages {
		ring := canvas.NewCircle(color.Transparent)
		ring.StrokeWidth = 1.6
		dot := canvas.NewCircle(primary)
		label := canvas.NewText(name, fg)
		label.TextSize = theme.TextSize()
		r.rows = append(r.rows, &stepRow{
			ring:  ring,
			dot:   dot,
			tick1: &canvas.Line{StrokeColor: success, StrokeWidth: 2},
			tick2: &canvas.Line{StrokeColor: success, StrokeWidth: 2},
			label: label,
		})
	}
	r.pulse = &fyne.Animation{
		Duration:    1150 * time.Millisecond,
		RepeatCount: fyne.AnimationRepeatForever,
		Curve:       fyne.AnimationLinear,
		Tick:        r.pulseTick,
	}
	if animationsOn() {
		r.pulse.Start()
	}
	r.applyAll()
	return r
}

type stepListRenderer struct {
	owner   *StepList
	rows    []*stepRow
	pulse   *fyne.Animation
	fg      color.NRGBA
	primary color.NRGBA
	success color.NRGBA
	active  int          // 当前脉冲的行
	ticked  map[int]bool // 已播过画勾动画的行
	laidOut bool
}

func (r *stepListRenderer) Destroy() { r.pulse.Stop() }

func (r *stepListRenderer) Layout(size fyne.Size) {
	for i, row := range r.rows {
		y := float32(i) * (stepRowHeight + stepRowGap)
		row.bx = 0
		row.by = y + (stepRowHeight-stepIconSize)/2
		row.ring.Move(fyne.NewPos(row.bx, row.by))
		row.ring.Resize(fyne.NewSquareSize(stepIconSize))
		inner := stepIconSize/2 - 5
		row.dot.Move(fyne.NewPos(row.bx+stepIconSize/2-inner/2, row.by+stepIconSize/2-inner/2))
		row.dot.Resize(fyne.NewSquareSize(inner))
		lh := row.label.MinSize().Height
		row.label.Move(fyne.NewPos(stepLabelX, y+(stepRowHeight-lh)/2))
		row.label.Refresh()
	}
	r.laidOut = true
	r.applyAll()
}

func (r *stepListRenderer) MinSize() fyne.Size {
	w := float32(180)
	for _, row := range r.rows {
		if lw := row.label.MinSize().Width; stepLabelX+lw > w {
			w = stepLabelX + lw
		}
	}
	h := float32(len(r.rows))*stepRowHeight + float32(len(r.rows)-1)*stepRowGap
	return fyne.NewSize(w, h)
}

func (r *stepListRenderer) Objects() []fyne.CanvasObject {
	objs := make([]fyne.CanvasObject, 0, len(r.rows)*5)
	for _, row := range r.rows {
		objs = append(objs, row.ring, row.dot, row.tick1, row.tick2, row.label)
	}
	return objs
}

func (r *stepListRenderer) Refresh() { r.applyAll() }

// applyAll 按 current 重排每一行的状态；新完成的行补播画勾动画。
func (r *stepListRenderer) applyAll() {
	cur := r.owner.current
	r.active = cur
	for i, row := range r.rows {
		switch {
		case i < cur: // 已完成
			row.ring.Hide()
			row.dot.Hide()
			row.label.Color = r.fg
			row.label.TextStyle = fyne.TextStyle{}
			if r.ticked[i] || !r.laidOut || !animationsOn() {
				r.ticked[i] = true
				r.drawTick(row, 1)
			} else {
				r.ticked[i] = true
				r.animateTick(i)
			}
		case i == cur: // 当前步
			row.ring.Hide()
			row.dot.Show()
			row.tick1.Hide()
			row.tick2.Hide()
			row.label.Color = r.fg
			row.label.TextStyle = fyne.TextStyle{Bold: true}
			row.dot.FillColor = r.primary
			row.dot.Refresh()
		default: // 未开始
			row.ring.Show()
			row.ring.StrokeColor = fadeAlpha(r.fg, 0x40)
			row.ring.Refresh()
			row.dot.Hide()
			row.tick1.Hide()
			row.tick2.Hide()
			row.label.Color = fadeAlpha(r.fg, 0x60)
			row.label.TextStyle = fyne.TextStyle{}
		}
		row.label.Refresh()
	}
}

// pulseTick 让当前步的圆点明暗脉动。
func (r *stepListRenderer) pulseTick(f float32) {
	if r.active < 0 || r.active >= len(r.rows) {
		return
	}
	dot := r.rows[r.active].dot
	if dot.Hidden {
		return
	}
	dot.FillColor = fadeAlpha(r.primary, uint8(0x3a+0xc5*sine01(f)))
	dot.Refresh()
}

// 对勾两笔在图标盒内的归一化端点。
var (
	tickP0 = fyne.NewPos(0.22, 0.53)
	tickP1 = fyne.NewPos(0.44, 0.75)
	tickP2 = fyne.NewPos(0.81, 0.28)
)

// tickPoint 把归一化端点换算成行内绝对坐标。
func tickPoint(row *stepRow, p fyne.Position) fyne.Position {
	return fyne.NewPos(row.bx+p.X*stepIconSize, row.by+p.Y*stepIconSize)
}

// drawTick 按进度画出对勾：t<0.5 描第一笔，之后描第二笔。
func (r *stepListRenderer) drawTick(row *stepRow, t float32) {
	p0, p1, p2 := tickPoint(row, tickP0), tickPoint(row, tickP1), tickPoint(row, tickP2)
	drawStroke(row.tick1, p0, p1, clamp01(t*2), r.success)
	drawStroke(row.tick2, p1, p2, clamp01((t-0.5)*2), r.success)
}

// animateTick 播放一次画勾动画。
func (r *stepListRenderer) animateTick(i int) {
	row := r.rows[i]
	anim := &fyne.Animation{
		Duration: 300 * time.Millisecond,
		Curve:    fyne.AnimationLinear,
		Tick:     func(f float32) { r.drawTick(row, f) },
	}
	anim.Start()
}

// drawStroke 把线段从 from 朝 to 描到 t 比例处；t 为 0 时隐藏。
func drawStroke(l *canvas.Line, from, to fyne.Position, t float32, col color.NRGBA) {
	if t <= 0 {
		l.Hide()
		return
	}
	l.Show()
	l.Position1 = from
	l.Position2 = fyne.NewPos(from.X+(to.X-from.X)*t, from.Y+(to.Y-from.Y)*t)
	l.StrokeColor = col
	l.Refresh()
}
