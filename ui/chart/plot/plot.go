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

// Axis is one of the chart's y scales: the first runs along the left side
// of the plot, the second along the right. Series name the axis they are
// measured on, so a millivolt channel and a volt channel each get a scale
// of their own.
// Band shades a range of y across the plot, under the series: a tolerance
// around a value, a safe operating area, a target range. Its Label, if any,
// goes at its top left.
type Band struct {
	Lo, Hi float64
	Axis   int      // the y axis the values are on; 0 when out of range
	Color  ui.Color // zero: the theme's accent, faint
	Label  string
}

type Axis struct {
	// Range fixes the axis; a zero Range fits it to the series on it.
	Range Range
	// Log makes the axis logarithmic, for values that span decades. The
	// fit rounds out to whole decades, and values at or below zero sit at
	// the bottom.
	Log bool
	// Format labels the ticks; nil labels them with as many decimals as
	// their step needs.
	Format func(float64) string
}

// oneAxis is the axis of a Chart that names none: a left one, fitted.
var oneAxis = [1]Axis{{}}

// Chart is a waveform view: its series, what it shows, and how it draws.
// Keep it in the app's state; it holds the buffers of every frame.
type Chart struct {
	Series []*Series
	Axes   []Axis        // zero: one left axis; at most two, left and right
	Bands  []Band        // shaded ranges of y, under the series
	Height float32       // zero: 200
	Time   bool          // x is seconds since the epoch, for the axis' ticks
	Follow time.Duration // >0: pin the newest this long, in seconds of x
	View   Viewport      // the user's zoom; a zero X fits the data
	Sync   *Group        // share the x view and the cursor with other charts
	Cursor  bool         // a crosshair at the pointer, reading the series there
	Legend  bool         // a row above the plot naming the series and their values
	Measure bool         // two markers with the change between them
	Label  string        // what assistive technology reads as the plot's name
	Note   string // its description, as "15 s at 1 kHz"

	vx      Range          // the x range of this frame
	p       *ui.Painter    // the painter of this frame
	r       ui.Rect        // the plot's box this frame
	drawFn  func(*ui.Painter, ui.Rect)      // ch.paint, made once: it escapes
	clipFn  func()                          // ch.emit, made once: it escapes
	inputFn func(ui.InputEvent) bool        // ch.input, made once: it escapes
	drag    dragState
	band    ui.Color // the drag's selection band
	cursorOn bool
	owns     bool    // this chart is the one with the pointer over it
	cursorX  float64 // the pointer's x, in data units
	mA, mB   float64 // the measurement markers, in data units
	mSet     bool    // the markers have been placed
	mDrag    int     // 0: none, 1: A, 2: B
	yFit     [2]bool // these axes were zoomed, so fitting clears them
	mark     ui.Color
	tint     ui.Color // the faint fill of a band
	px      int            // the plot's pixel width, from the last paint
	cache   []geom         // per series, keyed by the view, px and data version
	points  [][][2]float32 // per series, the screen points, reused
	yTicks  [2][]chart.Tick // one per axis
	xTicks  []chart.Tick
	yLo     [2]float64 // one per axis
	yHi     [2]float64
	yLog    [2]bool
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
	py    *pyramid // the levels of this series, kept across views
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
	head    int // a ring rotates in place: the same x is another sample then
}

// tickCache is the ticks of each axis and the domain they were for: a hit
// allocates nothing, which is what a steady frame needs.
type tickCache struct {
	y      [2]tickEntry
	bottom tickEntry
}

type tickEntry struct {
	lo, hi float64
	count  int
	timed  bool
	log    bool
	ticks  []chart.Tick
	ok     bool
}

// of returns the ticks of lo..hi on the axis, from the cache when the
// domain and the kind of scale are the same.
func (e *tickEntry) of(ax Axis, lo, hi float64, count int, timed bool) []chart.Tick {
	if e.ok && e.lo == lo && e.hi == hi && e.count == count && e.timed == timed && e.log == ax.Log {
		return e.ticks
	}
	switch {
	case timed:
		e.ticks = chart.NewTime(unix(lo), unix(hi)).Ticks(count)
	case ax.Log:
		l := chart.NewLog(lo, hi)
		l.Format = ax.Format
		e.ticks = l.Ticks(count)
	default:
		l := chart.NewLinear(lo, hi)
		l.Format = ax.Format
		e.ticks = l.Ticks(count)
	}
	e.lo, e.hi, e.count, e.timed, e.log, e.ok = lo, hi, count, timed, ax.Log, true
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
	for k := range ch.axisCount() {
		ch.yTicks[k] = ch.ticks.y[k].of(ch.axisAt(k), ch.yLo[k], ch.yHi[k], 4, false)
	}
	ch.xTicks = ch.ticks.bottom.of(Axis{}, vx.Lo, vx.Hi, 6, ch.Time)
	ch.border = c.Theme().Border
	ch.band = c.Theme().Accent.Alpha(0.18)
	ch.mark = c.Theme().TextMuted
	ch.tint = c.Theme().Accent.Alpha(0.12)
	ch.vx = vx
	ch.placeMarkers()
	if ch.drawFn == nil { // a steady frame must not allocate these
		ch.drawFn = ch.paint
		ch.clipFn = ch.emit
		ch.inputFn = ch.input
	}
	if ch.Legend || ch.Measure {
		return ui.Column(c).Gap(4).Children(func() {
			ch.topRow(c)
			ch.plot(c)
		})
	}
	return ch.plot(c)
}

// plot builds the grid, the axes and the area the series are drawn in.
func (ch *Chart) plot(c *ui.Context) ui.Element {
	height := ch.Height
	if height <= 0 {
		height = 200
	}
	ax := chart.Axes{Left: ch.yTicks[0], Bottom: ch.xTicks}
	if ch.axisCount() > 1 {
		ax.Right = ch.yTicks[1]
	}
	return chart.Plot(c, ax, func(area ui.Element) {
		area.Label(ch.Label).Description(ch.Note)
		area.Focusable().HandleInput(ch.inputFn)
		ch.keys(area)
		if ch.Cursor {
			ch.track(area)
		}
		if desc := ch.describe(); desc != "" {
			area.Value(desc)
		}
		area.Draw(ch.drawFn)
	}).Height(height).Gap(6)
}

// xView returns the range of x the chart shows: the follow window, the
// user's zoom, or the whole data, kept within half a screen of its ends.
func (ch *Chart) xView() Range {
	data := ch.dataExtent()
	v := ch.view()
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

// scanFor returns how to walk s for this view: its samples, or the buckets
// of the pyramid level that fits a pixel column, when the view covers more
// samples than the plot has pixels. An irregular x has no bucket grid.
func (ch *Chart) scanFor(g *geom, s *Series, vx Range, px int) scan {
	// A ring rotates under every view, and an irregular x has no bucket
	// grid: both decimate their samples directly.
	if s.X != nil || s.Head != 0 || s.Count() == 0 {
		return rawScan(s)
	}
	from := max(0, s.IndexAt(vx.Lo)-1)
	to := min(s.Count(), s.IndexAt(vx.Hi)+1)
	lv := levelFor(float64(to-from) / float64(max(1, px)))
	if lv < 0 {
		return rawScan(s)
	}
	if g.py == nil {
		g.py = &pyramid{}
	}
	g.py.build(s, lv)
	return bucketScan(s, g.py, lv)
}

// update decimates every series for vx at px pixels wide, and fits the y
// range to the extremes of the visible samples.
func (ch *Chart) update(vx Range, px int) {
	defer probe.Span("plot/update")()
	ch.align()
	var lo, hi [2]float64
	for k := range lo {
		lo[k], hi[k] = math.Inf(1), math.Inf(-1)
	}
	for i, s := range ch.Series {
		s.adopt()
		g := &ch.cache[i]
		key := geomKey{lo: vx.Lo, hi: vx.Hi, px: px, version: s.version, owner: ch.version,
			n: s.Count(), x0: s.X0, dx: s.Dx, head: s.Head}
		if !g.ready || g.key != key {
			g.key = key
			g.xs, g.lo, g.hi = columns(ch.scanFor(g, s, vx, px), vx, px, g.xs, g.lo, g.hi)
			g.ready = true
		}
		if s.Hidden {
			continue
		}
		a := ch.axisOf(s)
		for j, x := range g.xs {
			if x < vx.Lo || x > vx.Hi {
				continue // the points beyond each edge only feed the line
			}
			lo[a] = math.Min(lo[a], float64(g.lo[j]))
			hi[a] = math.Max(hi[a], float64(g.hi[j]))
		}
	}
	for k := range ch.axisCount() {
		ax := ch.axisAt(k)
		ch.yLog[k] = ax.Log
		ch.yLo[k], ch.yHi[k] = ch.fitAxis(k, lo[k], hi[k])
	}
}

// axisCount returns how many y axes the chart has: the ones it names, up to
// the left and right sides, or one fitted left axis when it names none.
func (ch *Chart) axisCount() int {
	if n := len(ch.Axes); n > 0 {
		return min(n, 2)
	}
	return 1
}

// axisAt returns the axis at index k.
func (ch *Chart) axisAt(k int) Axis {
	if len(ch.Axes) == 0 {
		return oneAxis[0]
	}
	return ch.Axes[k]
}

// axisOf returns the index of the axis a series is measured on.
func (ch *Chart) axisOf(s *Series) int {
	if a := s.Axis; a >= 0 && a < ch.axisCount() {
		return a
	}
	return 0
}

// fitAxis returns the y range of axis k, from the extremes of the samples
// visible on it: fixed when the axis says so, fitted and rounded otherwise.
func (ch *Chart) fitAxis(k int, lo, hi float64) (float64, float64) {
	ax := ch.axisAt(k)
	if ax.Range.Valid() {
		return ax.Range.Lo, ax.Range.Hi
	}
	if hi < lo { // nothing visible
		if ax.Log {
			return 1, 10
		}
		return 0, 1
	}
	if ax.Log { // whole decades, and never zero
		if lo <= 0 {
			lo = hi / 1000
		}
		if lo <= 0 {
			lo = 1
		}
		if hi <= lo {
			hi = lo * 10
		}
		return math.Pow(10, math.Floor(math.Log10(lo))), math.Pow(10, math.Ceil(math.Log10(hi)))
	}
	span := hi - lo
	if span == 0 {
		span = math.Abs(hi)*0.04 + 1e-9
	}
	l := chart.NewLinear(lo-0.04*span, hi+0.04*span)
	l.Format = ax.Format
	l = l.Nice(4)
	return l.Lo, l.Hi
}

// paint draws the grid, then every visible series, clipped to the plot.
func (ch *Chart) paint(p *ui.Painter, r ui.Rect) {
	defer probe.Span("plot/paint")()
	ch.p, ch.r = p, r
	if w := int(r.W * p.Scale()); w > 0 {
		ch.px = w
	}
	for _, t := range ch.yTicks[0] { // the grid follows the left axis
		y := chart.Y(r, t.At)
		p.Line(r.X, y, r.X+r.W, y, 1, ch.border)
	}
	ch.drawBands(p, r)
	ch.drawMarkers(p, r)
	if !ch.vx.Valid() || ch.yHi[0] <= ch.yLo[0] {
		return
	}
	if ch.cursorOn && ch.cursorX >= ch.vx.Lo && ch.cursorX <= ch.vx.Hi {
		f := float32((ch.cursorX - ch.vx.Lo) / ch.vx.Span())
		x := chart.X(r, f)
		p.Line(x, r.Y, x, r.Y+r.H, 1, ch.border)
		for i, s := range ch.Series {
			if s.Hidden {
				continue
			}
			if v, ok := ch.stamp(s); ok {
				y := chart.Y(r, ch.yf(v, ch.axisOf(s)))
				p.Fill(ui.Rect{X: x - 2, Y: y - 2, W: 4, H: 4}, color(i, s), 2)
			}
		}
	}
	if ch.drag.active && !ch.drag.pan { // the range a drag selects
		x0 := (ch.drag.fromX - ch.vx.Lo) / ch.vx.Span()
		x1 := (ch.drag.toX - ch.vx.Lo) / ch.vx.Span()
		lo, hi := min(x0, x1), max(x0, x1)
		if hi > lo {
			p.Fill(ui.Rect{X: chart.X(r, float32(lo)), Y: r.Y, W: float32(hi-lo) * r.W, H: r.H}, ch.band, 0)
		}
	}
	p.Clip(r, 0, ch.clipFn)
}

// yf maps a y value to a fraction up the plot on its axis, as the pointer
// sees it.
func (ch *Chart) yf(v float32, axis int) float32 {
	lo, hi := ch.yLo[axis], ch.yHi[axis]
	if ch.yLog[axis] {
		x := float64(v)
		if x <= 0 {
			x = lo
		}
		return float32((math.Log10(x) - math.Log10(lo)) / (math.Log10(hi) - math.Log10(lo)))
	}
	return float32((float64(v) - lo) / (hi - lo))
}

// emit draws the series' cached columns as screen points, inside the clip.
func (ch *Chart) emit() {
	p, r := ch.p, ch.r
	span := ch.vx.Span()
	for i, s := range ch.Series {
		if s.Hidden || i >= len(ch.cache) {
			continue
		}
		g := &ch.cache[i]
		if len(g.xs) == 0 {
			continue
		}
		pts := ch.points[i][:0]
		a := ch.axisOf(s)
		for j, x := range g.xs {
			f := float32((x - ch.vx.Lo) / span)
			pts = append(pts, [2]float32{chart.X(r, f), chart.Y(r, ch.yf(g.lo[j], a))})
			if g.hi[j] != g.lo[j] {
				pts = append(pts, [2]float32{chart.X(r, f), chart.Y(r, ch.yf(g.hi[j], a))})
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
}
