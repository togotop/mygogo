package plot

import (
	"math"
	"testing"
)

func TestSeriesIndexAt(t *testing.T) {
	s := Series{Y: make([]float32, 100), X0: 10, Dx: 0.5}
	// The first sample at or after x, clamped to the ends.
	for _, c := range []struct {
		x    float64
		want int
	}{
		{10, 0}, {9, 0}, {34.9, 50}, {35, 50}, {59.5, 99}, {100, 99},
	} {
		if got := s.IndexAt(c.x); got != c.want {
			t.Fatalf("IndexAt(%g) = %d, want %d", c.x, got, c.want)
		}
	}
}

func TestSeriesExtent(t *testing.T) {
	s := Series{Y: make([]float32, 5), X0: 2, Dx: 0.25}
	if got := s.Extent(); got != (Range{2, 3}) {
		t.Fatalf("Extent = %+v want 2,3", got)
	}
}

func TestSeriesIrregular(t *testing.T) {
	s := Series{Y: []float32{1, 2, 3}, X: []float64{0, 10, 30}}
	if got := s.IndexAt(10); got != 1 {
		t.Fatalf("IndexAt(10) = %d, want 1", got)
	}
	if s.Count() != 3 || s.XAt(2) != 30 {
		t.Fatalf("count %d x2 %g", s.Count(), s.XAt(2))
	}
}

func TestSeriesDegenerate(t *testing.T) {
	nan := float32(math.NaN())
	for _, c := range []struct {
		name string
		s    *Series
		x    float64
		want int
	}{
		{"single", &Series{Y: []float32{7}, X0: 0, Dx: 1}, 1e9, 0},
		{"all NaN", &Series{Y: []float32{nan, nan, nan}, X0: 0, Dx: 1}, 2, 2},
		{"flat Dx", &Series{Y: []float32{1, 2}, X0: 0, Dx: 0}, 5, 0},
		{"empty", &Series{}, 3, 0},
		{"negative x", &Series{Y: []float32{1, 2}, X0: 10, Dx: 1}, -5, 0},
	} {
		if got := c.s.IndexAt(c.x); got != c.want {
			t.Fatalf("%s: IndexAt(%g) = %d, want %d", c.name, c.x, got, c.want)
		}
	}
	if one := (Series{Y: []float32{7}, X0: 3, Dx: 1}); one.Extent() != (Range{3, 3}) {
		t.Fatalf("single sample extent %+v", one.Extent())
	}
}

func TestSeriesSwapFromAnotherGoroutine(t *testing.T) {
	s := &Series{Y: []float32{0}, X0: 0, Dx: 1}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 1; i <= 100; i++ {
			y := make([]float32, i)
			s.Swap(y, float64(i), 1)
		}
	}()
	for range 100 {
		s.adopt() // the view's once-a-frame adoption
	}
	<-done
	s.adopt()
	if s.X0 != 100 || len(s.Y) != 100 {
		t.Fatalf("adopted x0 %g n %d", s.X0, len(s.Y))
	}
}
