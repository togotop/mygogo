// Waveform draws four series of a million samples each: the plot reduces
// every one to the extremes of its pixel columns, so a frame costs the
// width of the plot, not the number of samples.
//
// Run it with -probe to print what the plot's spans cost once a second.
package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/internal/probe"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/chart/plot"
)

const (
	samples = 1_000_000
	rate    = 20_000 // sample rate, in Hz
)

// wave returns the i-th sample of channel s: two slow waves, some fine
// detail, and an isolated spike every so often that the columns must keep.
func wave(i, s int) float32 {
	x := float64(i) / rate
	v := .5 + .35*math.Sin(x*float64(s+1)*3) + .05*math.Sin(x*211)
	if i%99991 == s*7 {
		v += .4
	}
	return float32(v)
}

func channel(s int) []float32 {
	y := make([]float32, samples)
	for i := range y {
		y[i] = wave(i, s)
	}
	return y
}

func main() {
	probeOn := flag.Bool("probe", false, "print the plot's spans once a second")
	flag.Parse()

	ch := &plot.Chart{Height: 320, Label: "Four waves", Note: "four series of a million samples"}
	for s := range 4 {
		ch.Series = append(ch.Series, &plot.Series{
			Name: fmt.Sprintf("channel %d", s+1),
			Y:    channel(s),
			X0:   0,
			Dx:   1.0 / rate,
		})
	}

	if *probeOn {
		probe.On("plot") // the plot/update and plot/paint spans (a prefix, no slash)
		go func() {
			for range time.Tick(time.Second) {
				for _, n := range probe.Counts() {
					log.Printf("%-12s calls=%6d self=%10v max=%10v", n.Name, n.Calls,
						time.Duration(n.Self), time.Duration(n.Max))
				}
				probe.Reset() // one second's reading, not the process's
			}
		}()
	}

	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{Title: "Waveform", Width: 1200, Height: 700,
			Content: ui.View(func(c *ui.Context) {
				ui.Box(c).Fill().Padding(8).Children(func() { ch.Element(c) })
			})})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
