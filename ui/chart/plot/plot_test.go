package plot

import (
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestViewDraws(t *testing.T) {
	ch := &Chart{
		Height: 200,
		Series: []*Series{{Name: "a", Y: []float32{1, 4, 2, 5}, X0: 0, Dx: 1}},
	}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Fill().Children(func() { ch.Element(c) })
	}, 400, 300)
	tt.Frame()
	if ch.px == 0 {
		t.Fatal("the plot never measured its width")
	}
	if ch.yHi[0] < 5 {
		t.Fatalf("auto y = %g..%g, want to reach 5", ch.yLo[0], ch.yHi[0])
	}
	if !tt.HasText("6") { // y ticks of 0..6: x tops at "3.0", so 6 is y's
		t.Fatalf("texts: %v", tt.Texts())
	}
}

func TestAutoYFitsTheVisibleSamples(t *testing.T) {
	ch := &Chart{Series: []*Series{{Y: []float32{0, 50, 100, 0}, X0: 0, Dx: 1}}}
	ch.View = Viewport{X: Range{0, 1}} // the outlier at x=2 is out of view
	ch.update(Range{0, 1}, 1024)
	if ch.yHi[0] < 50 || ch.yHi[0] >= 100 {
		t.Fatalf("auto y = %g..%g, want the visible 0..50", ch.yLo[0], ch.yHi[0])
	}
}

func TestFollowPinsTheNewestWindow(t *testing.T) {
	// 100 samples 10 ms apart: the data covers 0.00..0.99. Follow shorter
	// than that, or Clamp would widen it back to the data.
	ch := &Chart{Follow: 500 * time.Millisecond,
		Series: []*Series{{Y: make([]float32, 100), X0: 0, Dx: 0.01}}}
	vx := ch.xView()
	if math.Abs(vx.Span()-0.5) > 1e-9 || math.Abs(vx.Hi-0.99) > 1e-9 {
		t.Fatalf("follow %+v, want 0.49..0.99", vx)
	}
}

func TestHiddenSeriesStayOutOfTheAutoY(t *testing.T) {
	ch := &Chart{Series: []*Series{
		{Y: []float32{0, 1}, X0: 0, Dx: 1},
		{Y: []float32{0, 1000}, X0: 0, Dx: 1, Hidden: true},
	}}
	ch.update(Range{0, 1}, 1024)
	if ch.yHi[0] >= 1000 {
		t.Fatalf("hidden series drove the y range: %g", ch.yHi[0])
	}
}
