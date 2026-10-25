package plot

import (
	"math"
	"testing"
)

// benchSeries returns n samples of a slow wave with noise, at 1 unit apart.
func benchSeries(n int) *Series {
	y := make([]float32, n)
	for i := range y {
		y[i] = float32(.5 + .4*math.Sin(float64(i)/500) + .05*float64(i%7))
	}
	return &Series{Y: y, X0: 0, Dx: 1}
}

// BenchmarkPlotColumns is the decimation budget: four series, 1280 pixels,
// the buffers carried from frame to frame as a view does. The 1% window at
// 1M samples is the one that must stay under 0.3 ms.
func BenchmarkPlotColumns(b *testing.B) {
	for _, c := range []struct {
		name string
		n    int
		span float64
	}{
		{"1m-1pct", 1_000_000, .01},
		{"1m-10pct", 1_000_000, .10},
		{"100k-10pct", 100_000, .10},
	} {
		b.Run(c.name, func(b *testing.B) {
			var series [4]*Series
			for i := range series {
				series[i] = benchSeries(c.n)
			}
			lo := 0.4 * float64(c.n)
			vx := Range{lo, lo + c.span*float64(c.n)}
			var xs [4][]float64
			var low, high [4][]float32
			b.ReportAllocs()
			for b.Loop() {
				for i := range series {
					xs[i], low[i], high[i] = columns(series[i], vx, 1280, xs[i], low[i], high[i])
				}
			}
		})
	}
}
