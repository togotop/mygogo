package plot

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// A marker snaps to the sample it is nearest, so a period is a whole
// number of samples and the readout is exact.
func TestMeasureSnapsToSamples(t *testing.T) {
	ch, tt := measureChart(t) // 101 samples at 1 apart
	from := chartX(ch, ch.mA) + 23.4 // between samples
	tt.Move(from, ch.r.Y+ch.r.H/2)
	tt.Press(from, ch.r.Y+ch.r.H/2)
	tt.Move(from, ch.r.Y+ch.r.H/2)
	tt.Release(from, ch.r.Y+ch.r.H/2)
	tt.Frame()
	if got := ch.mA - math.Round(ch.mA); math.Abs(got) > 1e-6 {
		t.Fatalf("marker A at %g, off the sample grid", ch.mA)
	}
	if got := ch.mB - math.Round(ch.mB); math.Abs(got) > 1e-6 {
		t.Fatalf("marker B at %g, off the sample grid", ch.mB)
	}
}

func TestMeasureDescribesItself(t *testing.T) {
	ch, tt := measureChart(t)
	tt.Move(ch.r.X+ch.r.W/2, ch.r.Y+ch.r.H/2)
	tt.Frame()
	if d := ch.describe(); !strings.Contains(d, "Δx") {
		t.Fatalf("the description says %q", d)
	}
}

// A view that follows new samples must not leave the markers behind: they
// measure the trace in front of them, so they keep their place on screen.
func TestMarkersKeepTheirPlaceInAFollowingView(t *testing.T) {
	s := &Series{Y: make([]float32, 100), X0: 0, Dx: .01}
	ch := &Chart{Height: 200, Measure: true, Follow: 500 * time.Millisecond, Series: []*Series{s}}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Fill().Children(func() { ch.Element(c) })
	}, 500, 300)
	tt.Frame()
	before := (ch.mA - ch.vx.Lo) / ch.vx.Span()
	s.Swap(make([]float32, 100), 0.5, .01) // a second of new samples
	tt.Frame()
	if after := (ch.mA - ch.vx.Lo) / ch.vx.Span(); math.Abs(after-before) > 1e-3 {
		t.Fatalf("the marker moved from %g to %g of the view", before, after)
	}
}

