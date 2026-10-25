// Package plot draws waveforms: series of samples fitted to a viewport and
// reduced to the extremes of each pixel column, so a line of a million
// samples costs a frame what a line of a thousand does.
//
// `ch := &plot.Chart{Series: []*plot.Series{{Y: ys, X0: 0, Dx: 0.001}}, Height: 240}`
// in the app's state, and `ch.Element(c)` in the view.
//
// The decimation is not chart.MinMax's: that takes fractions of the whole
// data, so a view has to remap every sample whenever the viewport moves
// (16 ms measured at a million samples even with 1% visible), and its x is
// float32, which quantizes seconds since the epoch to about two minutes.
// columns walks only the visible window, in data space, with x in float64.
//
// Keep the palette opaque: a translucent line falls back to StrokePath's
// CPU mask, which is much slower than the GPU capsules StrokePolyline draws.
//
// A Series belongs to the view's thread; another goroutine publishes new
// samples with Swap.
package plot

import (
	"math"
	"time"

	"github.com/egoist/mygo/internal/probe"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/chart"
)

// palette colors the series that have no color of their own, all opaque.
var palette = [...]ui.Color{
	ui.RGB(40, 140, 240),
	ui.RGB(240, 100, 70),
	ui.RGB(50, 190, 110),
	ui.RGB(200, 160, 40),
	ui.RGB(150, 90, 200),
	ui.RGB(80, 180, 200),
}

// Chart is a waveform view: its series, what it shows, and how it draws.
// Keep it in the app's state; it holds the buffers of every frame.
type Chart struct {
	Series []*Series
	Height float32       // zero: 200
	Time   bool          // x is seconds since the epoch, for the axis' ticks
	Follow time.Duration // >0: pin the newest this long, in seconds of x
	View   Viewport      // the user's zoom; a zero X fits the data
	Label  string        // what assistive technology reads as the plot's name
	Note   string // its description, as "15 s at 1 kHz"

	px      int            // the plot's pixel width, from the last paint
	cache   []geom         // per series, keyed by the view, px and data version
	points  [][][2]float32 // per series, the screen points, reused
	yTicks  []chart.Tick
	xTicks  []chart.Tick
	yLo     float64
	yHi     float64
	ticks   tickCache
	border  ui.Color
	version uint64 // what Touch bumps
}

// geom is one series' pixel columns in data space.
type geom struct {
	key   geomKey
	xs    []float64
	lo    []float32
	hi    []float32
	ready bool
}

// geomKey says when geom has to be decimated again: a new view, a new
// width, or new data. Style changes are not here: they only affect painting.
type geomKey struct {
	lo, hi  float64
	px      int
	version uint64
	owner   uint64 // the Chart's Touch
	n       int
	x0, dx  float64
}

// tickCache is the ticks of each axis and the domain they were for: a hit
// allocates nothing, which is what a steady frame needs.
type tickCache struct {
	left, bottom tickEntry
}

type tickEntry struct {
	lo, hi float64
	count  int
	timed  bool
	ticks  []chart.Tick
	ok     bool
}

// of returns the ticks of lo..hi, from the cache when the domain is the same.
func (e *tickEntry) of(lo, hi float64, count int, timed bool) []chart.Tick {
	if e.ok && e.lo == lo && e.hi == hi && e.count == count && e.timed == timed {
		return e.ticks
	}
	if timed {
		e.ticks = chart.NewTime(unix(lo), unix(hi)).Ticks(count)
	} else {
		e.ticks = chart.NewLinear(lo, hi).Ticks(count)
	}
	e.lo, e.hi, e.count, e.timed, e.ok = lo, hi, count, timed, true
	return e.ticks
}

// unix returns the time at seconds, for the time axis.
func unix(sec float64) time.Time { return time.Unix(0, int64(sec*1e9)) }

// Touch reports that the series, or their order, changed: the next frame
// decimates again.
func (ch *Chart) Touch() {
	ch.version++
	for _, s := range ch.Series {
		s.Touch()
	}
}

// Element builds the chart. Keep the Chart in the app's state and call
// this in the view, every frame.
func (ch *Chart) Element(c *ui.Context) ui.Element {
	vx := ch.xView()
	px := ch.px
	if px <= 0 {
		px = 1024
	}
	ch.update(vx, px)
	ch.yTicks = ch.ticks.left.of(ch.yLo, ch.yHi, 4, false)
	ch.xTicks = ch.ticks.bottom.of(vx.Lo, vx.Hi, 6, ch.Time)
	ch.border = c.Theme().Border
	height := ch.Height
	if height <= 0 {
		height = 200
	}
	return chart.Plot(c, chart.Axes{Left: ch.yTicks, Bottom: ch.xTicks}, func(area ui.Element) {
		area.Label(ch.Label).Description(ch.Note)
		area.Draw(func(p *ui.Painter, r ui.Rect) { ch.paint(p, r, vx) })
	}).Height(height).Gap(6)
}

// xView returns the range of x the chart shows: the follow window, the
// user's zoom, or the whole data, kept within half a screen of its ends.
func (ch *Chart) xView() Range {
	data := ch.dataExtent()
	v := ch.View
	if ch.Follow > 0 && data.Valid() {
		v = Viewport{X: Range{data.Hi - ch.Follow.Seconds(), data.Hi}}
	} else if !v.X.Valid() {
		v = v.Fit(data)
	}
	return v.Clamp(data).X
}

// dataExtent returns the x range all the visible series cover.
func (ch *Chart) dataExtent() Range {
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, s := range ch.Series {
		if s.Hidden || s.Count() == 0 {
			continue
		}
		e := s.Extent()
		lo, hi = math.Min(lo, e.Lo), math.Max(hi, e.Hi)
	}
	if lo > hi {
		return Range{}
	}
	return Range{lo, hi}
}

// align makes the cache and the point buffers one per series.
func (ch *Chart) align() {
	for len(ch.cache) < len(ch.Series) {
		ch.cache = append(ch.cache, geom{})
		ch.points = append(ch.points, nil)
	}
	ch.cache = ch.cache[:len(ch.Series)]
	ch.points = ch.points[:len(ch.Series)]
}

// update decimates every series for vx at px pixels wide, and fits the y
// range to the extremes of the visible samples.
func (ch *Chart) update(vx Range, px int) {
	defer probe.Span("plot/update")()
	ch.align()
	lo, hi := math.Inf(1), math.Inf(-1)
	for i, s := range ch.Series {
		s.adopt()
		g := &ch.cache[i]
		key := geomKey{lo: vx.Lo, hi: vx.Hi, px: px, version: s.version, owner: ch.version, n: s.Count(), x0: s.X0, dx: s.Dx}
		if !g.ready || g.key != key {
			g.key = key
			g.xs, g.lo, g.hi = columns(s, vx, px, g.xs, g.lo, g.hi)
			g.ready = true
		}
		if s.Hidden {
			continue
		}
		for j, x := range g.xs {
			if x < vx.Lo || x > vx.Hi {
				continue // the points beyond each edge only feed the line
			}
			lo = math.Min(lo, float64(g.lo[j]))
			hi = math.Max(hi, float64(g.hi[j]))
		}
	}
	switch {
	case ch.View.Y.Valid():
		ch.yLo, ch.yHi = ch.View.Y.Lo, ch.View.Y.Hi
	case hi < lo: // nothing visible
		ch.yLo, ch.yHi = 0, 1
	default:
		span := hi - lo
		if span == 0 {
			span = math.Abs(hi)*0.04 + 1e-9
		}
		l := chart.NewLinear(lo-0.04*span, hi+0.04*span).Nice(4)
		ch.yLo, ch.yHi = l.Lo, l.Hi
	}
}

// paint draws the grid, then every visible series, clipped to the plot.
func (ch *Chart) paint(p *ui.Painter, r ui.Rect, vx Range) {
	defer probe.Span("plot/paint")()
	if w := int(r.W * p.Scale()); w > 0 {
		ch.px = w
	}
	for _, t := range ch.yTicks {
		y := chart.Y(r, t.At)
		p.Line(r.X, y, r.X+r.W, y, 1, ch.border)
	}
	if !vx.Valid() || ch.yHi <= ch.yLo {
		return
	}
	ch.align()
	span := vx.Span()
	yf := func(v float32) float32 { return float32((float64(v) - ch.yLo) / (ch.yHi - ch.yLo)) }
	p.Clip(r, 0, func() {
		for i, s := range ch.Series {
			if s.Hidden || i >= len(ch.cache) {
				continue
			}
			g := &ch.cache[i]
			if len(g.xs) == 0 {
				continue
			}
			pts := ch.points[i][:0]
			for j, x := range g.xs {
				f := float32((x - vx.Lo) / span)
				pts = append(pts, [2]float32{chart.X(r, f), chart.Y(r, yf(g.lo[j]))})
				if g.hi[j] != g.lo[j] {
					pts = append(pts, [2]float32{chart.X(r, f), chart.Y(r, yf(g.hi[j]))})
				}
			}
			ch.points[i] = pts
			width, col := s.Width, s.Color
			if width <= 0 {
				width = 1.5
			}
			if col.A == 0 {
				col = palette[i%len(palette)]
			}
			p.StrokePolyline(pts, width, col)
		}
	})
}
