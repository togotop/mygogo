package plot

import (
	"math"
	"testing"
)

func TestPyramidBucketsHoldTheirExtremes(t *testing.T) {
	s := benchSeries(1 << 16) // 65,536 samples
	py := &pyramid{}
	py.build(s, 2) // levels 0..2
	for k, shift := range levelShifts[:3] {
		lo, hi := py.lo[k], py.hi[k]
		for b := 0; b < len(lo); b += 97 { // a sample of the buckets
			wantLo, wantHi := float32(math.NaN()), float32(math.NaN())
			for i := b << shift; i < min((b+1)<<shift, s.Count()); i++ {
				v := s.Y[i]
				if v != v {
					continue
				}
				if wantLo != wantLo || v < wantLo {
					wantLo = v
				}
				if wantHi != wantHi || v > wantHi {
					wantHi = v
				}
			}
			if lo[b] != wantLo || hi[b] != wantHi {
				t.Fatalf("level %d bucket %d = %g..%g, want %g..%g", k, b, lo[b], hi[b], wantLo, wantHi)
			}
		}
	}
}

func TestPyramidEnvelopeContainsRaw(t *testing.T) {
	s := benchSeries(1 << 16)
	vx := Range{0, float64(s.Count())}
	rawX, rawLo, rawHi := columns(rawScan(s), vx, 128, nil, nil, nil)
	py := &pyramid{}
	py.build(s, 1)
	bx, bLo, bHi := columns(bucketScan(s, py, 1), vx, 128, nil, nil, nil)
	if len(bx) == 0 || len(rawX) == 0 {
		t.Fatalf("no points: raw %d bucket %d", len(rawX), len(bx))
	}
	col := func(x float64) int {
		return max(0, min(127, int((x-vx.Lo)/vx.Span()*128)))
	}
	rawMin := make(map[int]float32, 128)
	rawMax := make(map[int]float32, 128)
	for i, x := range rawX {
		c := col(x)
		if v, ok := rawMin[c]; !ok || rawLo[i] < v {
			rawMin[c] = rawLo[i]
		}
		if v, ok := rawMax[c]; !ok || rawHi[i] > v {
			rawMax[c] = rawHi[i]
		}
	}
	// A bucket's column may cover samples of a neighbouring one, so its
	// envelope is a superset: after taking every bucket of a column, never
	// narrower, and never a lost spike.
	bMin := make(map[int]float32, 128)
	bMax := make(map[int]float32, 128)
	for i, x := range bx {
		c := col(x)
		if v, ok := bMin[c]; !ok || bLo[i] < v {
			bMin[c] = bLo[i]
		}
		if v, ok := bMax[c]; !ok || bHi[i] > v {
			bMax[c] = bHi[i]
		}
	}
	for c, lo := range rawMin {
		bl, ok := bMin[c]
		if !ok {
			continue // no bucket starts in that column: coarser, not wrong
		}
		if bl > lo || bMax[c] < rawMax[c] {
			t.Fatalf("column %d: buckets %g..%g miss raw %g..%g", c, bl, bMax[c], lo, rawMax[c])
		}
	}
}

func TestPyramidLevelFitsThePixel(t *testing.T) {
	for _, c := range []struct {
		perPixel float64
		want     int
	}{
		{10, -1}, {64, 0}, {512, 1}, {4096, 3}, {1e6, 5},
	} {
		if got := levelFor(c.perPixel); got != c.want {
			t.Fatalf("levelFor(%g) = %d, want %d", c.perPixel, got, c.want)
		}
	}
}
