package plot

import (
	"math"
	"strconv"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/chart"
)

// num formats a reading: dx, dy and the values at the markers.
func num(v float64) string { return strconv.FormatFloat(v, 'f', 3, 64) }

// placeMarkers puts A and B a third of the view apart the first time the
// chart measures, and keeps them where they are on screen while the view
// follows new samples: a marker measures the trace in front of it.
func (ch *Chart) placeMarkers() {
	if !ch.Measure || !ch.vx.Valid() {
		return
	}
	switch {
	case !ch.mSet:
		ch.mA = ch.snap(ch.vx.Lo + ch.vx.Span()/3)
		ch.mB = ch.snap(ch.vx.Lo + 2*ch.vx.Span()/3)
		ch.mSet = true
		ch.keepMarkers()
	case ch.Follow > 0:
		ch.mA = ch.vx.Lo + float64(ch.mFA)*ch.vx.Span()
		ch.mB = ch.vx.Lo + float64(ch.mFB)*ch.vx.Span()
	}
}

// keepMarkers records where the markers are along the view, which is where
// they stay when it follows new samples.
func (ch *Chart) keepMarkers() {
	if !ch.vx.Valid() || ch.vx.Span() <= 0 {
		return
	}
	ch.mFA = float32((ch.mA - ch.vx.Lo) / ch.vx.Span())
	ch.mFB = float32((ch.mB - ch.vx.Lo) / ch.vx.Span())
}

// markerLocal returns where a marker is, in the plot's own x.
func (ch *Chart) markerLocal(x float64) float32 {
	if !ch.vx.Valid() || ch.r.W <= 0 {
		return 0
	}
	return float32((x-ch.vx.Lo)/ch.vx.Span()) * ch.r.W
}

// grabMarker takes the marker within reach of local, if there is one: its
// drag moves that marker instead of zooming to a range.
func (ch *Chart) grabMarker(local float32) bool {
	if !ch.mSet {
		return false
	}
	const reach = 7 // DIPs
	da := math.Abs(float64(local) - float64(ch.markerLocal(ch.mA)))
	db := math.Abs(float64(local) - float64(ch.markerLocal(ch.mB)))
	switch {
	case da <= reach && da <= db:
		ch.mDrag = 1
	case db <= reach:
		ch.mDrag = 2
	default:
		return false
	}
	return true
}

// moveMarker drags the grabbed marker to local, on the sample grid.
func (ch *Chart) moveMarker(local float32) {
	x := ch.snap(ch.xAt(float64(local)))
	switch ch.mDrag {
	case 1:
		ch.mA = x
	case 2:
		ch.mB = x
	}
	ch.keepMarkers()
}

// snap moves x to the nearest sample of the first visible series that has
// an even grid: a marker on a sample reads that sample, and the distance
// between two of them is a whole number of samples.
func (ch *Chart) snap(x float64) float64 {
	for _, s := range ch.Series {
		if s.Hidden || s.Count() == 0 || s.X != nil || s.Dx <= 0 {
			continue
		}
		i := math.Round((x - s.X0) / s.Dx)
		i = math.Max(0, math.Min(float64(s.Count()-1), i))
		return s.X0 + i*s.Dx
	}
	return x
}

// drawMarkers draws the two markers and their letters.
func (ch *Chart) drawMarkers(p *ui.Painter, r ui.Rect) {
	if !ch.Measure || !ch.mSet {
		return
	}
	for _, m := range [2]struct {
		x float64
		s string
	}{{ch.mA, "A"}, {ch.mB, "B"}} {
		f := (m.x - ch.vx.Lo) / ch.vx.Span()
		if f < 0 || f > 1 {
			continue
		}
		x := chart.X(r, float32(f))
		p.Line(x, r.Y, x, r.Y+r.H, 1, ch.mark)
		p.Text(x+3, r.Y+10, m.s, 10, ch.mark)
	}
}

// delta returns what each series changes between the markers, and whether
// there is a measurement to read.
func (ch *Chart) delta(s *Series) (float32, bool) {
	if !ch.Measure || !ch.mSet {
		return 0, false
	}
	a, ok := ch.valueAt(s, ch.mA)
	if !ok {
		return 0, false
	}
	b, ok := ch.valueAt(s, ch.mB)
	if !ok {
		return 0, false
	}
	return b - a, true
}
