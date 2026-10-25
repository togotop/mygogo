package plot

import (
	"testing"
)

// A ring keeps the newest samples in a fixed buffer: Head says where the
// oldest one is, and the logical order wraps.
func TestRingYAt(t *testing.T) {
	s := &Series{Y: []float32{0, 1, 2, 3, 4}, Head: 3}
	want := []float32{3, 4, 0, 1, 2}
	for i, w := range want {
		if got := s.YAt(i); got != w {
			t.Fatalf("YAt(%d) = %g, want %g", i, got, w)
		}
	}
}

func TestRingColumnsStartAtTheOldestSample(t *testing.T) {
	// 10 slots, the oldest at 7: the samples in order are 100, 200, 300,
	// then 0..6.
	y := []float32{0, 1, 2, 3, 4, 5, 6, 100, 200, 300}
	s := &Series{Y: y, X0: 0, Dx: 1, Head: 7}
	xs, lo, _ := columns(rawScan(s), Range{0, 10}, 10, nil, nil, nil)
	if len(xs) < 10 {
		t.Fatalf("only %d points: %v", len(xs), xs)
	}
	if lo[0] != 100 || lo[1] != 200 || lo[2] != 300 || lo[3] != 0 {
		t.Fatalf("order %v, want 100 200 300 0", lo[:4])
	}
}
