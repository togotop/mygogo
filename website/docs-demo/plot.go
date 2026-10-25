package main

import (
	"math"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/chart/plot"
)

// The waveform demo: two channels of 2,000 samples with spikes between
// them, so the pixel columns and the measurement markers have something to
// show. The chart keeps its viewport across frames, as an app's does.
var (
	plotChart   = newPlotChart()
	plotMeasure bool
)

func newPlotChart() *plot.Chart {
	const n = 2000
	in := make([]float32, n)
	out := make([]float32, n)
	for i := range in {
		x := float64(i) / 200
		in[i] = float32(.5 + .35*math.Sin(x*3) + .03*math.Sin(x*57))
		out[i] = float32(.32 + .18*math.Sin(x*1.3+1) + .02*math.Sin(x*23))
		if i%431 == 200 {
			in[i] += .28 // a spike narrow enough to be lost without extremes
		}
	}
	return &plot.Chart{Height: 236, Cursor: true, Legend: true,
		Axes: []plot.Axis{{}},
		Series: []*plot.Series{
			{Name: "input", Y: in, X0: 0, Dx: 0.001},
			{Name: "output", Y: out, X0: 0, Dx: 0.001},
		}}
}

func init() {
	demo("plot", func(c *ui.Context) {
		t := c.Theme()
		plotChart.Measure = plotMeasure
		ui.Column(c).Gap(6).Children(func() {
			ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
				ui.Checkbox(c, &plotMeasure, "Measure").FontSize(12)
				ui.Text(c, "drag to zoom · shift-drag to pan · alt+wheel zooms y · double-click fits").
					FontSize(11).TextColor(t.TextMuted)
			})
			plotChart.Element(c)
		})
	})
}
