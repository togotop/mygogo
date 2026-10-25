package plot

import "math"

// columns reduces the samples of s between vx.Lo and vx.Hi to the lowest
// and highest of each pixel column of a plot px wide, in data space, with
// one point beyond each edge so the line reaches the border. It appends to
// the three slices and returns them, so a view keeping them reuses the
// storage.
//
// xs is float64: float32 seconds since the epoch lose sample precision.
func columns(s *Series, vx Range, px int, xs []float64, lo, hi []float32) ([]float64, []float32, []float32) {
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
