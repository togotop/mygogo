package plot

import "testing"

// BenchmarkPlotPyramid is the whole view of a large column: the levels are
// built once, and every frame scans buckets instead of samples. Touch makes
// each iteration decimate again, as a moving view does.
func BenchmarkPlotPyramid(b *testing.B) {
	for _, c := range []struct {
		name string
		n    int
	}{
		{"1m", 1_000_000},
		{"100k", 100_000},
	} {
		b.Run(c.name, func(b *testing.B) {
			s := benchSeries(c.n)
			ch := &Chart{Series: []*Series{s}}
			vx := s.Extent()
			ch.update(vx, 1280) // warm: build the levels
			b.ReportAllocs()
			for b.Loop() {
				ch.Touch()
				ch.update(vx, 1280)
			}
		})
	}
}

// BenchmarkPlotPyramidCold is the first frame at a million samples, the one
// that builds the levels from the raw samples.
func BenchmarkPlotPyramidCold(b *testing.B) {
	s := benchSeries(1_000_000)
	b.ReportAllocs()
	for b.Loop() {
		py := &pyramid{}
		py.build(s, 2)
	}
}
