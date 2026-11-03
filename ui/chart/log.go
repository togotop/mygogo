package chart

import (
	"math"
	"strconv"
)

// Log maps positive numbers from Lo to Hi onto the plot on a base-10 scale,
// as d3's scaleLog: each decade takes the same room, so 1 to 10 is as far
// as 100 to 1000.
//
// Lo and Hi must be positive: a log scale has no zero, and a value at or
// below zero is drawn at Lo.
type Log struct {
	Lo, Hi float64
	// Format labels the ticks; nil labels them the shortest way that is
	// exact, as "0.01", "1" and "100".
	Format func(v float64) string
}

// NewLog returns a log scale from lo to hi.
func NewLog(lo, hi float64) Log { return Log{Lo: lo, Hi: hi} }

// At returns where v is along the plot.
func (l Log) At(v float64) float32 {
	if !(l.Lo > 0) || !(l.Hi > l.Lo) {
		return 0
	}
	if v <= 0 {
		v = l.Lo
	}
	f := (math.Log10(v) - math.Log10(l.Lo)) / (math.Log10(l.Hi) - math.Log10(l.Lo))
	return float32(f)
}

// Span returns the stretch from a to b along the plot.
func (l Log) Span(a, b float64) Span { return Span{l.At(a), l.At(b)} }

// Invert returns the value at f along the plot, as where the pointer is.
func (l Log) Invert(f float32) float64 {
	if !(l.Lo > 0) || !(l.Hi > l.Lo) {
		return l.Lo
	}
	g := math.Log10(l.Lo) + float64(f)*(math.Log10(l.Hi)-math.Log10(l.Lo))
	return math.Pow(10, g)
}

// Ticks returns about count round values within the domain: whole decades
// when the span holds as many of them as count asks for, and the 2 to 9 of
// each decade when it does not.
func (l Log) Ticks(count int) []Tick {
	if !(l.Lo > 0) || !(l.Hi > l.Lo) || count <= 0 {
		return nil
	}
	lo, hi := math.Log10(l.Lo), math.Log10(l.Hi)
	var out []Tick
	add := func(v float64) {
		if v < l.Lo || v > l.Hi || len(out) >= 100 {
			return
		}
		out = append(out, Tick{At: l.At(v), Value: v, Label: l.label(v)})
	}
	// Whole decades when the span holds two or more of them; in a shorter
	// span they are too few to read the axis by, so 1, 2 and 5 fill it in.
	if int(math.Floor(hi))-int(math.Ceil(lo)) >= 2 {
		for e := int(math.Ceil(lo)); e <= int(math.Floor(hi)); e++ {
			add(math.Pow(10, float64(e)))
		}
		return out
	}
	for e := int(math.Floor(lo)); e <= int(math.Ceil(hi)); e++ {
		base := math.Pow(10, float64(e))
		for _, m := range []int{1, 2, 5} {
			add(float64(m) * base)
		}
	}
	return out
}

// label returns the tick's text.
func (l Log) label(v float64) string {
	if l.Format != nil {
		return l.Format(v)
	}
	if v >= 1e5 || v < 1e-3 {
		return strconv.FormatFloat(v, 'e', 0, 64)
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}
