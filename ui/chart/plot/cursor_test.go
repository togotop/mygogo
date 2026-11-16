package plot

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func cursorChart(t *testing.T, legend bool) (*Chart, *ui.Tester) {
	t.Helper()
	y := []float32{0, 10, 20, 30, 40}
	ch := &Chart{Height: 200, Cursor: true, Legend: legend,
		Series: []*Series{{Name: "channel", Y: y, X0: 0, Dx: 1}}}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Fill().Children(func() { ch.Element(c) })
	}, 400, 300)
	tt.Frame()
	return ch, tt
}

func TestCursorReadsTheSeriesUnderIt(t *testing.T) {
	ch, tt := cursorChart(t, false)
	tt.Move(ch.r.X+ch.r.W/2, ch.r.Y+ch.r.H/2)
	tt.Frame()
	if !ch.cursorOn {
		t.Fatal("no cursor while the pointer is over the plot")
	}
	if ch.cursorX < 1.9 || ch.cursorX > 2.1 {
		t.Fatalf("cursor at x %g, want the middle of 0..4", ch.cursorX)
	}
	if v, ok := ch.stamp(ch.Series[0]); !ok || v != 20 {
		t.Fatalf("read %g (ok %v), want 20", v, ok)
	}
	tt.Move(200, 290) // below the plot
	tt.Frame()
	if ch.cursorOn {
		t.Fatal("the cursor stayed after the pointer left")
	}
}

func TestLegendShowsTheValueUnderTheCursor(t *testing.T) {
	ch, tt := cursorChart(t, true)
	if !tt.HasText("channel") {
		t.Fatalf("no series name in the legend: %v", tt.Texts())
	}
	tt.Move(ch.r.X+ch.r.W/2, ch.r.Y+ch.r.H/2)
	tt.Frame()
	if !tt.HasText("20.000") {
		t.Fatalf("no value in the legend: %v", tt.Texts())
	}
}

func TestLegendSkipsHiddenSeries(t *testing.T) {
	y := []float32{1, 2, 3}
	ch := &Chart{Height: 120, Legend: true, Series: []*Series{
		{Name: "shown", Y: y, X0: 0, Dx: 1},
		{Name: "hidden", Y: y, X0: 0, Dx: 1, Hidden: true},
	}}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Fill().Children(func() { ch.Element(c) })
	}, 400, 300)
	tt.Frame()
	if !tt.HasText("shown") {
		t.Fatalf("legend: %v", tt.Texts())
	}
	if tt.HasText("hidden") {
		t.Fatal("a hidden series is in the legend")
	}
}
