# Waveforms

Package `ui/chart/plot` draws series of samples — waveforms — whose cost
follows the width of the plot rather than the number of samples: every frame
reduces the visible samples to the lowest and highest of each pixel column,
so a million samples cost a frame what a thousand do.

It is the finished part of what [Charts](chart.md) gives as parts: `chart`
offers scales, axes and marks to build any chart from, and `plot` builds one
kind of chart properly — a viewport, a cursor, a legend and a pyramid for the
samples far off screen.

```go
import "github.com/egoist/mygo/ui/chart/plot"

ch := &plot.Chart{
	Height: 240,
	Cursor: true,
	Legend: true,
	Series: []*plot.Series{
		{Name: "ch1", Y: mic1, X0: 0, Dx: 1.0 / 20_000}, // 20 kHz
		{Name: "ch2", Y: mic2, X0: 0, Dx: 1.0 / 20_000},
	},
}
// in the view, every frame:
ch.Element(c)
```

## A chart

`Chart` holds the series, what the view shows and the buffers of a frame, so
keep one in the app's state and call `Element(c)` in the view. It draws its
own grid, axes and ticks: the y axis fits the visible samples, rounded out to
round values, and the x axis comes from the viewport.

```go
ch.Height = 320      // zero: 200
ch.Time = true       // x is seconds since the epoch: the x axis reads as a clock
ch.Label = "Two channels"
ch.Note = "20 kHz for 15 s"
```

Positions are data, not fractions: `X0` and `Dx` place the samples, so
panning and zooming never rewrites them.

## Series

A `Series` is a column of samples equally spaced in x from `X0` by `Dx`, or
at the x values in `X` when it is not nil, for an irregular series. A NaN in
`Y` breaks the line there.

```go
s := &plot.Series{
	Name:  "temperature",
	Y:     samples,        // []float32, NaN for a gap
	X0:    start,          // the first sample's x, in seconds or anything
	Dx:    0.001,          // 1 ms between samples
	Color: ui.RGB(40, 140, 240), // zero: the palette's, by index
	Width: 1.5,            // zero: 1.5 DIPs
	Hidden: false,
	Axis:  0,              // measured on the chart's first axis
}
```

The chart never copies the samples: it reads them where they are. A series
belongs to the view's thread — change the slices there and call
`Series.Touch`, which is what makes the next frame decimate again. A new
length, `X0` or `Dx` is noticed without it; new values in the same places
are not.

## Axes

One y axis, fitted to every series and drawn on the left, is the default. A
channel of millivolts and one of volts cannot share it — the small one
flattens — so a series names the axis it is measured on: the first runs
along the left, the second along the right, each fitted to its own series.

```go
ch.Axes = []plot.Axis{
	{},                         // fitted to the series on it
	{Range: plot.Range{-5, 5}}, // fixed
}
ch.Series[1].Axis = 1
```

`Axis.Format` labels the ticks its way, as `Axis.Format: func(v float64) string {
return strconv.FormatFloat(v, 'f', 1, 64) + " V" }`. `Axis.Log` makes the axis
logarithmic: the fit rounds out to whole decades, values at or below zero sit
at the bottom, and Alt with the wheel zooms it in decades. The grid follows
the first axis; the second only labels its side.

## Live data

`Follow` pins the view to the newest samples: a chart of a feed shows the
last few seconds without the app moving the viewport itself.

```go
ch.Follow = 5 * time.Second // in seconds of x
```

A producer on another goroutine publishes a whole column with `Swap`, which
is safe to call from anywhere; the view adopts it on its next frame. The
slice it publishes must not be written again — build a new one:

```go
func (a *acq) pump() {
	next := make([]float32, a.window)
	// fill next from the device ...
	a.series.Swap(next, a.x0, a.dx) // from the acquisition goroutine
}
```

A new column does not by itself wake the view: the view asks for frames
while data is arriving, as any live view does.

```go
Content: ui.View(func(c *ui.Context) {
	c.AnimationFrame()
	ch.Element(c)
})
```

A fixed buffer that wraps is a ring: `Head` says where the oldest sample is,
and the i-th sample is `Y[(Head+i)%len(Y)]`. `YAt(i)` reads it in that order
for the app, and the plot decimates it without a copy.

## Zoom and pan

The plot takes its input as it comes:

| Gesture | What it does |
|---|---|
| Drag | Zooms to the x range it selects (a band shows it) |
| Shift + drag | Pans |
| Double click | Fits the data again |
| Ctrl or Cmd + wheel | Zooms around the pointer, a quarter per notch |
| Alt + wheel | Zooms the y axis under the pointer, the left half or the right |
| Pinch | Zooms around the pointer |
| ← →, + −, Home, Escape | Pans, zooms around the middle, fits again |

Fitting the view (a double click, Home or Escape) also fits the y axes a
wheel zoomed, and keeps the ones the app fixed with `Axis.Range`.

Without the modifier the wheel goes on to the scroll containers around the
chart, so a waveform in a scrolling page still scrolls. `Viewport` is what
the chart shows, and its methods are pure, so the app can zoom too:

```go
ch.View = Viewport{X: Range{10, 20}}      // a second, in data units
ch.View = ch.View.ZoomX(0.5, 15)          // half as wide, around 15
ch.View = ch.View.PanX(-2)                // two units left
ch.View = Viewport{}                      // zero: fit the data
```

`Clamp` keeps a view within half a screen of the data at each end, which is
what panning past the edge settles to.

## Cursor and legend

`Cursor` draws a crosshair at the pointer and a dot on each series where it
is, and tells assistive technology what it is on. `Legend` puts a row above
the plot with each series' color, name and the value it has under the
cursor. Both are off by default; a chart of live data usually wants both:

```go
ch.Cursor, ch.Legend = true, true
```

## Measuring between markers

`Measure` puts two markers a third of the view apart. Dragging one moves it;
everything else on the plot still zooms and pans, and a press within a few
pixels of a marker drags it instead. A marker snaps to the nearest sample of
the first series with an even grid, so the distance between two is a whole
number of samples—the period of a wave, when the markers are one cycle apart.
The row above the plot reads the change in x, and each series' change between
the markers, as an oscilloscope's cursors do.

```go
ch.Measure = true
ch.Legend = true // each series' Δ between the markers
```

## Charts that move together

Charts that name the same `Group` share one x view and one cursor: a drag in
one zooms them all, and a crosshair in one lines up with the others, as
uPlot's `sync` does. Each fits its own y range to its own series.

```go
g := &plot.Group{}
top := &plot.Chart{Height: 200, Sync: g, Series: voltage, Cursor: true}
below := &plot.Chart{Height: 120, Sync: g, Series: current, Cursor: true}
```

## What it costs

Measured on an i7-13650HX with `ui.Tester` (CPU fallback, 1280 pixels wide):
four series of a million samples, 1% of them visible, decimate in **190 µs** a
frame; the whole million in view, through the pyramid, in **43 µs**; a steady
frame allocates nothing for its geometry — decimation, mapping and emission —
and three allocations in all, from the axis layout of `ui/chart`, whether it
draws a million samples or none.

Samples decimate to at most two points per pixel column, so a line never
carries more than the plot can show, and a narrow spike is never lost: the
extremes of each column are what is drawn.

A live frame—four channels at 20 kHz, a second pinned, a new column per
frame—is about **1 ms** and 18 allocations, most of them the ticks a moving
view recomputes (`BenchmarkPlotStreamingFrame`).

The first frame at a million samples builds the pyramid: about 2.7 ms a
series, once. A series that grows every frame decimates its samples
directly — the pyramid is for a column that stays put.

Keep the palette opaque: a translucent line falls back to `StrokePath`'s CPU
mask, which is much slower than the GPU capsules that `StrokePolyline` draws.

## What it leaves out

More than two y axes, bands and annotations, and exporting an image. `ui/chart` has the parts for those; `plot` keeps to
the waveform.
