package plot

import (
	"math"
	"testing"
)

func TestColumnsKeepSpikes(t *testing.T) {
	ys := make([]float32, 10000)
	for i := range ys {
		ys[i] = float32(i % 100)
	}
	ys[1234] = 5000 // one narrow spike, 1/10000 of the data
	s := &Series{Y: ys, X0: 0, Dx: 1}
	xs, _, hi := columns(rawScan(s), Range{0, 10000}, 100, nil, nil, nil)
	if len(xs) < 200 || len(xs) > 202 {
		t.Fatalf("emitted %d columns, want 2 per pixel", len(xs))
	}
	found := false
	for i := range xs {
		if hi[i] == 5000 {
			found = true
		}
	}
	if !found {
		t.Fatal("the spike is gone")
	}
}

func TestColumnsStayNearTheView(t *testing.T) {
	ys := []float32{0, 99, 0, 0, 0, 0}
	s := &Series{Y: ys, X0: 0, Dx: 1}
	xs, _, _ := columns(rawScan(s), Range{2, 3}, 10, nil, nil, nil)
	// At most one sample beyond each edge feeds the line into the border;
	// no more is drawn. Auto y ignores the ones outside the view.
	for i := range xs {
		if xs[i] < 1 || xs[i] > 3 {
			t.Fatalf("x %g too far outside the view", xs[i])
		}
	}
}

func TestColumnsBreakOnNaN(t *testing.T) {
	ys := []float32{0, 0, float32(math.NaN()), 0, 0}
	s := &Series{Y: ys, X0: 0, Dx: 1}
	xs, _, _ := columns(rawScan(s), Range{0, 5}, 5, nil, nil, nil)
	for i := 1; i < len(xs); i++ {
		if xs[i]-xs[i-1] > 1.5 { // the gap at the NaN is visible
			return
		}
	}
	t.Fatalf("no gap: %v", xs)
}
