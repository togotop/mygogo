package plot

import "math"

// scan walks a series in x order: its samples, or the buckets of a pyramid
// level. columns emits from a scan, so the raw path and the bucketed one
// share one emitter and differ only in what an item covers.
type scan struct {
	s     *Series
	lv    int       // -1: the raw samples
	lo    []float32 // the buckets' extremes, when lv >= 0
	hi    []float32
	shift uint
	n     int // how many buckets
}

// rawScan scans the samples themselves.
func rawScan(s *Series) scan { return scan{s: s, lv: -1} }

// bucketScan scans the buckets of one level of py.
func bucketScan(s *Series, py *pyramid, lv int) scan {
	return scan{s: s, lv: lv, lo: py.lo[lv], hi: py.hi[lv], shift: levelShifts[lv], n: len(py.lo[lv])}
}

func (sc scan) count() int {
	if sc.lv < 0 {
		return sc.s.Count()
	}
	return sc.n
}

// x returns the x an item starts at.
func (sc scan) x(i int) float64 {
	if sc.lv < 0 {
		return sc.s.XAt(i)
	}
	return sc.s.X0 + float64(i<<sc.shift)*sc.s.Dx
}

func (sc scan) low(i int) float32 {
	if sc.lv < 0 {
		return sc.s.Y[i]
	}
	return sc.lo[i]
}

func (sc scan) high(i int) float32 {
	if sc.lv < 0 {
		return sc.s.Y[i]
	}
	return sc.hi[i]
}

// indexAt returns the first item at or after x.
func (sc scan) indexAt(x float64) int {
	if sc.lv < 0 {
		return sc.s.IndexAt(x)
	}
	return sc.s.IndexAt(x) >> sc.shift
}

// columns reduces the visible part of a scan to the lowest and highest of
// each pixel column of a plot px wide, in data space, with one point
// beyond each edge so the line reaches the border. It appends to the three
// slices and returns them, so a view keeping them reuses the storage.
//
// xs is float64: float32 seconds since the epoch lose sample precision.
func columns(sc scan, vx Range, px int, xs []float64, lo, hi []float32) ([]float64, []float32, []float32) {
	if sc.lv < 0 {
		return columnsRaw(sc.s, vx, px, xs, lo, hi)
	}
	return columnsBuckets(sc, vx, px, xs, lo, hi)
}

// columnsRaw is the fast path: one sample, one y read, no item abstraction.
// It must keep the same envelope as columnsBuckets (tested).
func columnsRaw(s *Series, vx Range, px int, xs []float64, lo, hi []float32) ([]float64, []float32, []float32) {
	if s.Head == 0 {
		return columnsLinear(s, vx, px, xs, lo, hi)
	}
	return columnsRing(s, vx, px, xs, lo, hi)
}

// columnsLinear decimates a plain column: sample i is Y[i].
func columnsLinear(s *Series, vx Range, px int, xs []float64, lo, hi []float32) ([]float64, []float32, []float32) {
	xs, lo, hi = xs[:0], lo[:0], hi[:0]
	n := s.Count()
	if n == 0 || px <= 0 || !vx.Valid() {
		return xs, lo, hi
	}
	from := max(0, s.IndexAt(vx.Lo)-1)
	to := min(n, s.IndexAt(vx.Hi)+1)
	scale := float64(px) / vx.Span()
	col := func(i int) int { return int(math.Floor((s.XAt(i) - vx.Lo) * scale)) }
	emit := func(i int) {
		xs = append(xs, s.XAt(i))
		lo = append(lo, s.Y[i])
		hi = append(hi, s.Y[i])
	}
	for i := from; i < to; {
		if s.Y[i] != s.Y[i] { // NaN breaks the line
			i++
			continue
		}
		c := col(i)
		low, high := i, i
		i++
		for i < to && s.Y[i] == s.Y[i] && col(i) == c {
			if s.Y[i] < s.Y[low] {
				low = i
			}
			if s.Y[i] > s.Y[high] {
				high = i
			}
			i++
		}
		a, b := low, high // in sample order, as chart.MinMax emits them
		if a > b {
			a, b = b, a
		}
		emit(a)
		if b != a {
			emit(b)
		}
	}
	return xs, lo, hi
}

// columnsRing decimates a ring: the sample at logical i sits at the slot p,
// which a ring wraps at most once over the window, so p steps beside i
// rather than taking a remainder per sample.
func columnsRing(s *Series, vx Range, px int, xs []float64, lo, hi []float32) ([]float64, []float32, []float32) {
	xs, lo, hi = xs[:0], lo[:0], hi[:0]
	n := s.Count()
	if n == 0 || px <= 0 || !vx.Valid() {
		return xs, lo, hi
	}
	from := max(0, s.IndexAt(vx.Lo)-1)
	to := min(n, s.IndexAt(vx.Hi)+1)
	scale := float64(px) / vx.Span()
	y, yn := s.Y, len(s.Y)
	p := from + s.Head
	if p >= yn {
		p -= yn
	}
	col := func(i int) int { return int(math.Floor((s.XAt(i) - vx.Lo) * scale)) }
	next := func() { if p++; p == yn {
		p = 0
	} }
	for i := from; i < to; {
		if y[p] != y[p] { // NaN breaks the line
			i++
			next()
			continue
		}
		c := col(i)
		lowI, highI := i, i
		lowP, highP := p, p
		i++
		next()
		for i < to && y[p] == y[p] && col(i) == c {
			if y[p] < y[lowP] {
				lowI, lowP = i, p
			}
			if y[p] > y[highP] {
				highI, highP = i, p
			}
			i++
			next()
		}
		a, b := lowI, highI // in sample order, as chart.MinMax emits them
		ap, bp := lowP, highP
		if a > b {
			a, b, ap, bp = b, a, bp, ap
		}
		xs = append(xs, s.XAt(a))
		lo = append(lo, y[ap])
		hi = append(hi, y[ap])
		if b != a {
			xs = append(xs, s.XAt(b))
			lo = append(lo, y[bp])
			hi = append(hi, y[bp])
		}
	}
	return xs, lo, hi
}

// columnsBuckets decimates the buckets of a level: each item already holds
// the extremes of up to a pixel column's worth of samples.
func columnsBuckets(sc scan, vx Range, px int, xs []float64, lo, hi []float32) ([]float64, []float32, []float32) {
	xs, lo, hi = xs[:0], lo[:0], hi[:0]
	n := sc.count()
	if n == 0 || px <= 0 || !vx.Valid() {
		return xs, lo, hi
	}
	from := max(0, sc.indexAt(vx.Lo)-1)
	to := min(n, sc.indexAt(vx.Hi)+1)
	scale := float64(px) / vx.Span()
	emit := func(i int) {
		xs = append(xs, sc.x(i))
		lo = append(lo, sc.lo[i])
		hi = append(hi, sc.hi[i])
	}
	col := 0
	for i := from; i < to; {
		if sc.lo[i] != sc.lo[i] { // a bucket of nothing but NaN
			i++
			continue
		}
		col = int(math.Floor((sc.x(i) - vx.Lo) * scale))
		loIdx, hiIdx := i, i
		loV, hiV := sc.lo[i], sc.hi[i]
		i++
		for i < to && sc.lo[i] == sc.lo[i] && int(math.Floor((sc.x(i)-vx.Lo)*scale)) == col {
			if sc.lo[i] < loV {
				loV, loIdx = sc.lo[i], i
			}
			if sc.hi[i] > hiV {
				hiV, hiIdx = sc.hi[i], i
			}
			i++
		}
		a, b := loIdx, hiIdx
		if a > b {
			a, b = b, a
		}
		emit(a)
		if b != a {
			emit(b)
		}
	}
	return xs, lo, hi
}
