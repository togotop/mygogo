package plot

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
)

// groupCharts stacks two charts that share one x view and cursor.
func groupCharts(t *testing.T) (*Group, *Chart, *Chart, *ui.Tester) {
	t.Helper()
	g := &Group{}
	mk := func() *Chart {
		y := make([]float32, 100)
		for i := range y {
			y[i] = float32(i % 10)
		}
		return &Chart{Height: 120, Sync: g, Cursor: true,
			Series: []*Series{{Y: y, X0: 0, Dx: 1}}}
	}
	a, b := mk(), mk()
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Children(func() {
			a.Element(c)
			b.Element(c)
		})
	}, 400, 400)
	tt.Frame()
	return g, a, b, tt
}

func TestGroupSharesTheZoom(t *testing.T) {
	_, a, b, tt := groupCharts(t)
	span0 := a.vx.Span()
	tt.WheelWith(ui.Ctrl, 200, a.r.Y+a.r.H/2, 0, -100)
	tt.Frame()
	if a.vx.Span() >= span0 {
		t.Fatalf("no zoom: %g -> %g", span0, a.vx.Span())
	}
	if math.Abs(a.vx.Lo-b.vx.Lo) > 1e-9 || math.Abs(a.vx.Hi-b.vx.Hi) > 1e-9 {
		t.Fatalf("views differ: %+v vs %+v", a.vx, b.vx)
	}
}

func TestGroupSharesTheCursor(t *testing.T) {
	_, a, b, tt := groupCharts(t)
	tt.Move(a.r.X+a.r.W/2, a.r.Y+a.r.H/2)
	tt.Frame()
	if !b.cursorOn {
		t.Fatal("the other chart shows no cursor")
	}
	if math.Abs(a.cursorX-b.cursorX) > 1e-9 {
		t.Fatalf("cursors differ: %g vs %g", a.cursorX, b.cursorX)
	}
	// Leaving the group's charts clears it for both.
	tt.Move(5, 395)
	tt.Frame()
	if b.cursorOn || a.cursorOn {
		t.Fatal("the cursor stayed after the pointer left")
	}
}
