package plot

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
)

func logChart(t *testing.T) (*Chart, *ui.Tester) {
	t.Helper()
	ch := &Chart{Height: 200, Axes: []Axis{{Log: true}},
		Series: []*Series{{Y: []float32{1, 10, 100, 1000}, X0: 0, Dx: 1}}}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Fill().Children(func() { ch.Element(c) })
	}, 500, 300)
	tt.Frame()
	return ch, tt
}

func TestLogAxisFitsWholeDecades(t *testing.T) {
	ch, tt := logChart(t)
	if ch.yLo[0] != 1 || ch.yHi[0] != 1000 {
		t.Fatalf("log axis fitted to %g..%g", ch.yLo[0], ch.yHi[0])
	}
	if f := ch.yf(10, 0); math.Abs(float64(f)-1.0/3) > 1e-5 {
		t.Fatalf("10 sits at %g of the height, want a third", f)
	}
	if !tt.HasText("100") {
		t.Fatalf("no decade ticks: %v", tt.Texts())
	}
}

func TestLogAxisWithAZero(t *testing.T) {
	ch := &Chart{Height: 200, Axes: []Axis{{Log: true}},
		Series: []*Series{{Y: []float32{0, 1, 100}, X0: 0, Dx: 1}}}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Fill().Children(func() { ch.Element(c) })
	}, 500, 300)
	tt.Frame()
	if !(ch.yLo[0] > 0) {
		t.Fatalf("log axis starts at %g", ch.yLo[0])
	}
	if f := ch.yf(0, 0); f != f || f < -0.01 || f > 0.01 {
		t.Fatalf("a zero sits at %g, want the bottom", f)
	}
}

func TestAltWheelZoomsALogAxisInDecades(t *testing.T) {
	ch, tt := logChart(t)
	mid := func() float64 {
		return math.Pow(10, (math.Log10(ch.yLo[0])+math.Log10(ch.yHi[0]))/2)
	}
	before := mid()
	tt.WheelWith(ui.Alt, ch.r.X+ch.r.W*0.25, ch.r.Y+ch.r.H/2, 0, -100)
	tt.Frame()
	if math.Abs(mid()-before) > before*0.05 {
		t.Fatalf("the value at the pointer moved: %g -> %g", before, mid())
	}
	if ch.yHi[0]/ch.yLo[0] >= 1000 {
		t.Fatalf("the axis did not zoom: %g..%g", ch.yLo[0], ch.yHi[0])
	}
}
