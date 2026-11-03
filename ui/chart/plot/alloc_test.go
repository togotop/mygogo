package plot

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
)

// steadyChart is three series of 15,000 samples with the whole data in view:
// what a live chart looks like between samples.
func steadyChart() (*Chart, *ui.Tester) {
	series := make([]*Series, 3)
	for i := range series {
		y := make([]float32, 15000)
		for j := range y {
			y[j] = float32(math.Sin(float64(j)*0.01 + float64(i)))
		}
		series[i] = &Series{Y: y, X0: 0, Dx: 0.001}
	}
	ch := &Chart{Height: 240, Series: series}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Fill().Children(func() { ch.Element(c) })
	}, 900, 500)
	return ch, tt
}

// TestGeometryDoesNotAllocate is the decimation, mapping and emission: the
// buffers live on the Chart and every frame fills them again.
func TestGeometryDoesNotAllocate(t *testing.T) {
	ch, tt := steadyChart()
	tt.Frame()
	tt.Frame()
	if n := testing.AllocsPerRun(50, func() { ch.update(ch.xView(), ch.px) }); n > 0 {
		t.Fatalf("%v allocations in a frame's geometry", n)
	}
}

// TestSteadyFrameAllocationFloor: a fixed view and unchanged data costs the
// same three allocations whether it draws a million samples or none—they are
// chart.Plot's axis layout (its track slices), not the plot's own work.
func TestSteadyFrameAllocationFloor(t *testing.T) {
	_, tt := steadyChart()
	for range 300 {
		tt.Frame()
	}
	if n := testing.AllocsPerRun(50, tt.Frame); n > 3 {
		t.Fatalf("%v allocations per frame, want at most chart.Plot's three", n)
	}

	empty := &Chart{Height: 240}
	tt2 := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Fill().Children(func() { empty.Element(c) })
	}, 900, 500)
	for range 300 {
		tt2.Frame()
	}
	if n := testing.AllocsPerRun(50, tt2.Frame); n != 3 {
		t.Fatalf("empty chart: %v allocations, want the same three", n)
	}
}

// TestMovingViewAllocationStaysBounded: a view that moves every frame makes
// the ticks recompute (Follow does this), which allocates their labels—but a
// bounded number, and not one per sample.
func TestMovingViewAllocationStaysBounded(t *testing.T) {
	ch, tt := steadyChart()
	tt.Frame()
	off := 0.0
	n := testing.AllocsPerRun(50, func() {
		off += 0.001
		ch.View = Viewport{X: Range{off, off + 5}}
		tt.Frame()
	})
	if n > 30 {
		t.Fatalf("%v allocations for a moving view", n)
	}
}
