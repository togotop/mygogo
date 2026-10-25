package plot

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func twoAxisChart(t *testing.T) (*Chart, *ui.Tester) {
	t.Helper()
	small := &Series{Name: "millivolts", Y: []float32{0, .5, 1}, X0: 0, Dx: 1}
	big := &Series{Name: "volts", Y: []float32{0, 500, 1000}, X0: 0, Dx: 1, Axis: 1}
	ch := &Chart{Height: 200, Axes: []Axis{{}, {}}, Series: []*Series{small, big}}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Fill().Children(func() { ch.Element(c) })
	}, 500, 300)
	tt.Frame()
	return ch, tt
}

func TestAxisFitsItsOwnSeries(t *testing.T) {
	ch, _ := twoAxisChart(t)
	if hi := ch.yHi[0]; hi < 1 || hi > 2 {
		t.Fatalf("axis 0 fits to %g, want about 1", hi)
	}
	// Nice rounds 0..1000 out to -500..1500, as d3's does: what matters is
	// that it is not fitted to the millivolt series' 0..1.
	if hi := ch.yHi[1]; hi < 1000 || hi > 2000 {
		t.Fatalf("axis 1 fits to %g, want about 1000", hi)
	}
}

func TestTwoAxesDrawTheirOwnTicks(t *testing.T) {
	_, tt := twoAxisChart(t)
	if !tt.HasText("0.5") { // the left axis, fitted to 0..1
		t.Fatalf("no left axis ticks: %v", tt.Texts())
	}
	if !tt.HasText("1000") { // the right axis, fitted to 0..1000
		t.Fatalf("no right axis ticks: %v", tt.Texts())
	}
}

func TestFixedAxisRangeIsKept(t *testing.T) {
	ch := &Chart{Height: 200, Axes: []Axis{{Range: Range{0, 10}}},
		Series: []*Series{{Y: []float32{0, 500, 1000}, X0: 0, Dx: 1}}}
	ch.update(Range{0, 2}, 1024)
	if ch.yLo[0] != 0 || ch.yHi[0] != 10 {
		t.Fatalf("fixed range became %g..%g", ch.yLo[0], ch.yHi[0])
	}
}

func TestSeriesAxisOutOfRangeFallsBack(t *testing.T) {
	ch := &Chart{Height: 200, Axes: []Axis{{}},
		Series: []*Series{{Y: []float32{0, 1}, X0: 0, Dx: 1, Axis: 3}}}
	ch.update(Range{0, 1}, 1024)
	if hi := ch.yHi[0]; hi < 1 || hi > 2 {
		t.Fatalf("axis 0 fits to %g", hi)
	}
}
