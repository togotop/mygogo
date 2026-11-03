package plot

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
)

func measureChart(t *testing.T) (*Chart, *ui.Tester) {
	t.Helper()
	y := make([]float32, 101)
	for i := range y {
		y[i] = float32(i)
	}
	ch := &Chart{Height: 200, Measure: true, Axes: []Axis{{}},
		Series: []*Series{{Name: "ramp", Y: y, X0: 0, Dx: 1}}}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Fill().Children(func() { ch.Element(c) })
	}, 500, 300)
	tt.Frame()
	return ch, tt
}

// markerAt returns the window x of a marker, as the last frame drew it.
func markerAt(ch *Chart, x float64) float32 {
	return chartX(ch, x)
}

func chartX(ch *Chart, x float64) float32 {
	return ch.r.X + float32((x-ch.vx.Lo)/ch.vx.Span())*ch.r.W
}

func TestMeasureStartsTwoMarkers(t *testing.T) {
	ch, _ := measureChart(t)
	if math.IsNaN(ch.mA) || math.IsNaN(ch.mB) {
		t.Fatalf("markers not placed: %g %g", ch.mA, ch.mB)
	}
	if ch.mA >= ch.mB {
		t.Fatalf("markers %g %g are not in order", ch.mA, ch.mB)
	}
	if d := ch.mB - ch.mA; math.Abs(d-ch.vx.Span()/3) > ch.vx.Span()*0.05 {
		t.Fatalf("markers %g apart, want about a third of the view", d)
	}
}

func TestMeasureDragsAMarker(t *testing.T) {
	ch, tt := measureChart(t)
	from := chartX(ch, ch.mA)
	to := from + 60
	tt.Move(from, ch.r.Y+ch.r.H/2)
	tt.Press(from, ch.r.Y+ch.r.H/2)
	tt.Move(to, ch.r.Y+ch.r.H/2)
	tt.Release(to, ch.r.Y+ch.r.H/2)
	tt.Frame()
	if math.Abs(float64(ch.mA)-ch.vx.Lo-float64(to-ch.r.X)/float64(ch.r.W)*ch.vx.Span()) > ch.vx.Span()*0.02 {
		t.Fatalf("marker A at %g after the drag", ch.mA)
	}
}

func TestMeasureReadout(t *testing.T) {
	_, tt := measureChart(t)
	if !tt.HasText("Δx") {
		t.Fatalf("no Δx readout: %v", tt.Texts())
	}
}

func TestMeasureDragIsNotAZoom(t *testing.T) {
	ch, tt := measureChart(t)
	span := ch.vx.Span()
	from := chartX(ch, ch.mA)
	tt.Press(from, ch.r.Y+ch.r.H/2)
	tt.Move(from+80, ch.r.Y+ch.r.H/2)
	tt.Release(from+80, ch.r.Y+ch.r.H/2)
	tt.Frame()
	if ch.vx.Span() != span {
		t.Fatalf("dragging a marker zoomed: %g -> %g", span, ch.vx.Span())
	}
}
