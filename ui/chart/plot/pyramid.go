package plot

import "math"

// The bucket sizes of the pyramid: 64, 256, 1024, ... samples each, four
// times the one before.
var levelShifts = []uint{6, 8, 10, 12, 14, 16}

// pyramid holds the extremes of buckets of samples: level k's bucket b
// covers samples [b<<shift, (b+1)<<shift) of the series it was built for.
// It is built on demand, and rebuilt when the samples change.
type pyramid struct {
	lo, hi [][]float32
	n      int         // the samples it covers
	x0, dx float64     // the grid it was built on
}

// levelFor returns the level whose buckets are no wider than the samples
// one pixel column covers, or -1 for the raw samples: coarser would lose
// detail the column has room to show.
func levelFor(perPixel float64) int {
	k := -1
	for i, sh := range levelShifts {
		if float64(uint64(1)<<sh) <= perPixel {
			k = i
		}
	}
	return k
}

// build makes sure the levels up to level exist for s.
func (py *pyramid) build(s *Series, level int) {
	if py.n != s.Count() || py.x0 != s.X0 || py.dx != s.Dx {
		py.lo, py.hi = py.lo[:0], py.hi[:0]
		py.n, py.x0, py.dx = s.Count(), s.X0, s.Dx
	}
	for len(py.lo) <= level {
		py.lo = append(py.lo, nil)
		py.hi = append(py.hi, nil)
	}
	for k := 0; k <= level; k++ {
		if py.lo[k] == nil {
			py.buildLevel(s, k)
		}
	}
}

// buildLevel fills one level: from the samples for the first, from the
// level before for the rest, four of its buckets to one.
func (py *pyramid) buildLevel(s *Series, k int) {
	nan := float32(math.NaN())
	sh := levelShifts[k]
	if k == 0 {
		n := (s.Count() + (1<<sh) - 1) >> sh
		lo, hi := make([]float32, n), make([]float32, n)
		for b := range n {
			l, h := nan, nan
			for i := b << sh; i < min((b+1)<<sh, s.Count()); i++ {
				v := s.Y[i]
				if v != v {
					continue
				}
				if l != l || v < l {
					l = v
				}
				if h != h || v > h {
					h = v
				}
			}
			lo[b], hi[b] = l, h
		}
		py.lo[0], py.hi[0] = lo, hi
		return
	}
	prevLo, prevHi := py.lo[k-1], py.hi[k-1]
	n := (len(prevLo) + 3) >> 2
	lo, hi := make([]float32, n), make([]float32, n)
	for b := range n {
		l, h := nan, nan
		for j := b << 2; j < min((b+1)<<2, len(prevLo)); j++ {
			if v := prevLo[j]; v == v && (l != l || v < l) {
				l = v
			}
			if v := prevHi[j]; v == v && (h != h || v > h) {
				h = v
			}
		}
		lo[b], hi[b] = l, h
	}
	py.lo[k], py.hi[k] = lo, hi
}
