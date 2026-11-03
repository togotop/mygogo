package plot

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestAltWheelZoomsY(t *testing.T) {
	ch, tt := twoAxisChart(t)
	span0 := ch.yHi[0] - ch.yLo[0]
	mid := ch.yLo[0] + (ch.yHi[0]-ch.yLo[0])/2 // the value at half height
	tt.WheelWith(ui.Alt, ch.r.X+ch.r.W*0.25, ch.r.Y+ch.r.H/2, 0, -100)
	tt.Frame()
	span := ch.yHi[0] - ch.yLo[0]
	if span >= span0*0.95 {
		t.Fatalf("span %g -> %g: no y zoom", span0, span)
	}
	at := ch.yLo[0] + (ch.yHi[0]-ch.yLo[0])/2
	if math.Abs(at-mid) > span*0.05 {
		t.Fatalf("the value at the pointer moved: %g -> %g", mid, at)
	}
	if ch.Axes[0].Range != (Range{ch.yLo[0], ch.yHi[0]}) {
		t.Fatalf("the axis did not become fixed: %+v", ch.Axes[0].Range)
	}
}

func TestAltWheelZoomsTheAxisUnderThePointer(t *testing.T) {
	ch, tt := twoAxisChart(t)
	left0, right0 := ch.yHi[0]-ch.yLo[0], ch.yHi[1]-ch.yLo[1]
	tt.WheelWith(ui.Alt, ch.r.X+ch.r.W*0.75, ch.r.Y+ch.r.H/2, 0, -100) // the right half: axis 1
	tt.Frame()
	if d := ch.yHi[1] - ch.yLo[1]; d >= right0*0.95 {
		t.Fatalf("the right axis did not zoom: %g -> %g", right0, d)
	}
	if d := ch.yHi[0] - ch.yLo[0]; math.Abs(d-left0) > left0*1e-9 {
		t.Fatalf("the left axis changed: %g -> %g", left0, d)
	}
}

func TestDoubleClickFitsYAgain(t *testing.T) {
	ch, tt := twoAxisChart(t)
	span0 := ch.yHi[0] - ch.yLo[0]
	tt.WheelWith(ui.Alt, ch.r.X+ch.r.W*0.25, ch.r.Y+ch.r.H/2, 0, -100)
	tt.Frame()
	if ch.yHi[0]-ch.yLo[0] >= span0 {
		t.Fatal("no zoom to undo")
	}
	tt.ClickAt(ch.r.X+ch.r.W*0.25, ch.r.Y+ch.r.H/2)
	tt.ClickAt(ch.r.X+ch.r.W*0.25, ch.r.Y+ch.r.H/2)
	tt.Frame()
	if span := ch.yHi[0] - ch.yLo[0]; math.Abs(span-span0) > span0*1e-6 {
		t.Fatalf("fit left the span at %g, want %g", span, span0)
	}
}

func TestFitKeepsTheRangeTheAppFixed(t *testing.T) {
	ch, tt := twoAxisChart(t)
	ch.Axes[1].Range = Range{0, 10}
	tt.Frame()
	if ch.yLo[1] != 0 || ch.yHi[1] != 10 {
		t.Fatalf("fixed axis at %g..%g", ch.yLo[1], ch.yHi[1])
	}
	tt.ClickAt(ch.r.X+ch.r.W*0.5, ch.r.Y+ch.r.H/2)
	tt.ClickAt(ch.r.X+ch.r.W*0.5, ch.r.Y+ch.r.H/2)
	tt.Frame()
	if ch.yLo[1] != 0 || ch.yHi[1] != 10 {
		t.Fatalf("fit cleared a fixed axis: %g..%g", ch.yLo[1], ch.yHi[1])
	}
}
