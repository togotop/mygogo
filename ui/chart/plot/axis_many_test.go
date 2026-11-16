package plot

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
)

// Three series at three scales get three axes: left, right, and one more
// column to the right, each fitted to its own series.
func TestThreeAxes(t *testing.T) {
	mk := func(lo, hi float32, axis int) *Series {
		return &Series{Y: []float32{lo, hi}, X0: 0, Dx: 1, Axis: axis}
	}
	ch := &Chart{Height: 200, Axes: []Axis{{Title: "a"}, {Title: "b"}, {Title: "c"}},
		Series: []*Series{mk(0, 1, 0), mk(0, 10, 1), mk(0, 1000, 2)}}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Fill().Children(func() { ch.Element(c) })
	}, 600, 300)
	tt.Frame()
	for _, title := range []string{"a", "b", "c"} {
		if !tt.HasText(title) {
			t.Fatalf("axis %q has no title: %v", title, tt.Texts())
		}
	}
	if hi := ch.yHi[2]; hi < 1000 || hi > 2000 {
		t.Fatalf("axis 2 fits to %g, want about 1000", hi)
	}
	// The third axis maps its own values: 500 is halfway up its decades of
	// room, not at the top as it would be on 0..1.
	f := ch.yf(500, 2)
	if f < 0.2 || f > 0.6 {
		t.Fatalf("500 sits at %g of axis 2, want near the middle", f)
	}
	if math.Abs(float64(ch.yf(1, 0))) > 0.75 {
		t.Fatalf("axis 0's 1 sits at %g", ch.yf(1, 0))
	}
}

func TestAxisCountIsOneWithoutAxes(t *testing.T) {
	ch := &Chart{Series: []*Series{{Y: []float32{0, 1}, X0: 0, Dx: 1}}}
	if n := ch.axisCount(); n != 1 {
		t.Fatalf("axisCount = %d", n)
	}
}
