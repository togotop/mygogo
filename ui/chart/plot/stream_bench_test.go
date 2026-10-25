package plot

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// BenchmarkPlotStreamingFrame is a live chart as an app draws it: four
// channels at 20 kHz, a second pinned to the newest samples, a column
// published with Swap before every frame as a device thread does.
func BenchmarkPlotStreamingFrame(b *testing.B) {
	const (
		rate = 20_000
		n    = rate // a second
	)
	ch := &Chart{Height: 320, Cursor: true, Legend: true, Follow: time.Second,
		Axes: []Axis{{}, {}}}
	for c := range 4 {
		ch.Series = append(ch.Series, &Series{
			Name: fmt.Sprintf("ch%d", c+1), X0: 0, Dx: 1.0 / rate, Axis: c / 2,
		})
	}
	var bufs [2][4][]float32
	for k := range bufs {
		for c := range bufs[k] {
			bufs[k][c] = make([]float32, n)
			for i := range bufs[k][c] {
				bufs[k][c][i] = float32(.5 + .4*math.Sin(float64(i+c*17)/300))
			}
		}
	}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Fill().Children(func() { ch.Element(c) })
	}, 1080, 800)
	tt.Frame()
	x0, k := 0.0, 0
	b.ReportAllocs()
	for b.Loop() {
		k ^= 1 // the next column was filled while the view drew the last
		for c, s := range ch.Series {
			s.Swap(bufs[k][c], x0, 1.0/rate)
		}
		x0 += 333.0 / rate
		tt.Frame()
	}
}
