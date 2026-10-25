package plot

import (
	"math"
	"sort"
	"sync/atomic"

	"github.com/egoist/mygo/ui"
)

// Range is a stretch of the x or y axis, in data units.
type Range struct {
	Lo, Hi float64
}

// Span returns how much of the axis the range covers.
func (r Range) Span() float64 { return r.Hi - r.Lo }

// Valid reports whether the range has a direction: Hi is above Lo.
func (r Range) Valid() bool { return r.Hi > r.Lo }

// Series is one line of the chart: samples equally spaced in x from X0 by
// Dx, or at the x values in X when it is not nil. X must ascend; a NaN in
// Y breaks the line there.
//
// A Series belongs to the thread that runs the view: change its fields
// there and call Touch. Another goroutine publishes a whole new column
// with Swap; the view adopts it on its next frame.
type Series struct {
	Name string
	Y     []float32
	X0    float64
	Dx    float64
	X     []float64 // irregular x, ascending; nil means X0 + i*Dx

	Color  ui.Color // zero: the palette's, by index; keep it opaque
	Width  float32  // zero: 1.5
	Fill   ui.Color // zero: none
	Hidden bool

	version uint64 // what Touch bumps; an adopted swap bumps it too
	pending atomic.Pointer[swapped]
}

// swapped is a column published from another goroutine.
type swapped struct {
	y      []float32
	x0, dx float64
}

// Touch reports that Y or X changed in place. A new length, X0 or Dx is
// detected without it; the values are not.
func (s *Series) Touch() { s.version++ }

// Swap publishes a new column of samples from any goroutine. The slice
// must not be written after the call: build the next one afresh.
func (s *Series) Swap(y []float32, x0, dx float64) {
	s.pending.Store(&swapped{y: y, x0: x0, dx: dx})
}

// adopt takes in a pending swap, if any. The view calls it once a frame.
func (s *Series) adopt() {
	if p := s.pending.Swap(nil); p != nil {
		s.Y, s.X0, s.Dx, s.X = p.y, p.x0, p.dx, nil
		s.version++
	}
}

// Count returns how many samples the series has.
func (s *Series) Count() int { return len(s.Y) }

// XAt returns the x of the i-th sample.
func (s *Series) XAt(i int) float64 {
	if s.X != nil {
		return s.X[i]
	}
	return s.X0 + float64(i)*s.Dx
}

// IndexAt returns the index of the first sample at or after x, clamped to
// the ends: the sample under a point of the axis. Both paths agree on
// this, the uniform one by ceil and the irregular one by binary search.
func (s *Series) IndexAt(x float64) int {
	n := s.Count()
	if n == 0 {
		return 0
	}
	if s.X == nil {
		if s.Dx <= 0 {
			return 0
		}
		i := int(math.Ceil((x - s.X0) / s.Dx))
		return max(0, min(n-1, i))
	}
	i := sort.Search(n, func(i int) bool { return s.X[i] >= x })
	return min(max(0, i), n-1)
}

// Extent returns the range of x the series covers.
func (s *Series) Extent() Range {
	if s.Count() == 0 {
		return Range{}
	}
	return Range{s.XAt(0), s.XAt(s.Count() - 1)}
}
