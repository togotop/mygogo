package plot

import (
	"math"
	"strings"
	"testing"
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
