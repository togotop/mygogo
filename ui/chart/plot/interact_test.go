package plot

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
)

// interactChart is a chart of 100 samples at 1 apart, in a 400×300 window.
func interactChart(t *testing.T) (*Chart, *ui.Tester) {
	t.Helper()
	y := make([]float32, 100)
	for i := range y {
		y[i] = float32(i % 10)
	}
	ch := &Chart{Height: 200, Series: []*Series{{Y: y, X0: 0, Dx: 1}}}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Fill().Children(func() { ch.Element(c) })
	}, 400, 300)
	tt.Frame()
	return ch, tt
}

// under returns the data x at the window's x, as the last frame drew it.
func under(ch *Chart, windowX float64) float64 {
	return ch.vx.Lo + (windowX-float64(ch.r.X))/float64(ch.r.W)*ch.vx.Span()
}

func TestCtrlWheelZoomsUnderThePointer(t *testing.T) {
	ch, tt := interactChart(t)
	span0 := ch.vx.Span()
	at := under(ch, 200)
	tt.WheelWith(ui.Ctrl, 200, 100, 0, -100) // a notch up: zoom in
	tt.Frame()
	if span := ch.vx.Span(); span >= span0*0.95 {
		t.Fatalf("span %g -> %g: no zoom", span0, span)
	}
	if d := math.Abs(under(ch, 200) - at); d > ch.vx.Span()*0.02 {
		t.Fatalf("the point under the pointer moved by %g", d)
	}
}

func TestWheelWithoutTheModifierLeavesTheView(t *testing.T) {
	ch, tt := interactChart(t)
	span0 := ch.vx.Span()
	tt.WheelWith(0, 200, 100, 0, -100)
	tt.Frame()
	if span := ch.vx.Span(); span != span0 {
		t.Fatalf("a bare wheel zoomed: %g -> %g", span0, span)
	}
}

func TestDragZoomsToTheRangeItSelects(t *testing.T) {
	ch, tt := interactChart(t)
	span0 := ch.vx.Span()
	tt.Move(150, 100)
	tt.Press(150, 100)
	tt.Move(250, 100)
	tt.Release(250, 100)
	tt.Frame()
	want := 100 / float64(ch.r.W) * span0
	if span := ch.vx.Span(); math.Abs(span-want) > want*0.1 {
		t.Fatalf("span %g, want about %g", span, want)
	}
}

func TestShiftDragPans(t *testing.T) {
	ch, tt := interactChart(t)
	tt.WheelWith(ui.Ctrl, 200, 100, 0, -100) // zoom in: a view of the whole data cannot pan
	tt.Frame()
	span0, lo0 := ch.vx.Span(), ch.vx.Lo
	tt.PressWith(ui.Shift, 200, 100)
	tt.Move(250, 100)
	tt.Release(250, 100)
	tt.Frame()
	if span := ch.vx.Span(); math.Abs(span-span0) > 1e-6 {
		t.Fatalf("a pan changed the span: %g -> %g", span0, span)
	}
	want := 50 / float64(ch.r.W) * span0
	if got := lo0 - ch.vx.Lo; math.Abs(got-want) > want*0.1 {
		t.Fatalf("panned by %g, want %g", got, want)
	}
}

func TestDoubleClickFits(t *testing.T) {
	ch, tt := interactChart(t)
	full := ch.vx.Span()
	tt.WheelWith(ui.Ctrl, 200, 100, 0, -100)
	tt.Frame()
	zoomed := ch.vx.Span()
	tt.ClickAt(200, 100)
	tt.ClickAt(200, 100)
	tt.Frame()
	if span := ch.vx.Span(); math.Abs(span-full) > 1e-6 || span <= zoomed {
		t.Fatalf("double click: %g -> %g, want %g", zoomed, span, full)
	}
}

func TestPinchZooms(t *testing.T) {
	ch, tt := interactChart(t)
	span0 := ch.vx.Span()
	tt.Pinch(200, 100, 2)
	tt.Frame()
	if span := ch.vx.Span(); math.Abs(span-span0/2) > span0*0.05 {
		t.Fatalf("span %g -> %g, want about %g", span0, span, span0/2)
	}
}
