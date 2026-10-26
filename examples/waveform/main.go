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
	"os"
	"path/filepath"
	"runtime"
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

// sample returns the i-th sample of channel s: the last two channels are
// in millivolts, on their own axis.
func sample(i, s int) float32 {
	if s < 2 {
		return wave(i, s)
	}
	return wave(i, s) * 1000
}

func channel(s int) []float32 {
	y := make([]float32, samples)
	for i := range y {
		y[i] = sample(i, s)
	}
	return y
}

var (
	probeOn = flag.Bool("probe", false, "print the plot's spans once a second")
	live    = flag.Bool("live", false, "stream a second of samples at a time instead of a static minute")

	measure bool   // the two markers
	showBand bool  // the tolerance band
	saved   string // what the last export did
)

// band is the tolerance the demo shades: the input's expected range.
var band = []plot.Band{{Lo: 0.3, Hi: 0.7, Label: "expected"}}

// feed publishes a new second of samples for every channel, 30 times a
// second, as a device thread does: a fresh column each time, which the view
// adopts on its next frame (Series.Swap is safe from any goroutine). A
// device that knows the view is done with a column can reuse a pair of
// buffers instead; this demo keeps the contract simple.
func feed(ch *plot.Chart) {
	const rate = 20_000
	start := time.Now()
	for now := range time.Tick(33 * time.Millisecond) {
		due := int64(now.Sub(start).Seconds() * rate)
		from := max(int64(0), due-rate) // the last second
		if n := int(due - from); n > 0 {
			for c := range ch.Series {
				y := make([]float32, n)
				for i := range y {
					y[i] = sample(int(from)+i, c)
				}
				ch.Series[c].Swap(y, float64(from)/rate, 1.0/rate)
			}
		}
	}
}

func main() {
	flag.Parse()
	run()
}

// toolbar holds the switches the demo shows off, and the export.
func toolbar(c *ui.Context, ch *plot.Chart) {
	t := c.Theme()
	ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Checkbox(c, &measure, "Measure").FontSize(12)
		ui.Checkbox(c, &showBand, "Bands").FontSize(12)
		if ui.Button(c, "Save PNG").Clicked() {
			saved = export(ch)
		}
	})
	if saved != "" {
		ui.Text(c, saved).FontSize(11).TextColor(t.TextMuted)
	}
}

// save writes the chart into dir as a PNG, and says what it did.
func save(ch *plot.Chart, dir string) string {
	b, err := ch.PNG(1600, 900)
	if err != nil {
		return "export failed: " + err.Error()
	}
	p := filepath.Join(dir, "waveform.png")
	if err := os.WriteFile(p, b, 0o644); err != nil {
		return "export failed: " + err.Error()
	}
	log.Printf("waveform: saved %s (%d KB)", p, len(b)/1024)
	return fmt.Sprintf("saved %s (%d KB)", p, len(b)/1024)
}

// export saves the chart where the app keeps its data.
func export(ch *plot.Chart) string {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return "export failed: " + err.Error()
	}
	return save(ch, dir)
}

// run is the app: main on a desktop, the Android host's entry on a phone,
// where there is no command line and the readings print by default.
func run() {
	ch := &plot.Chart{Height: 320, Cursor: true, Legend: true,
		Axes:  []plot.Axis{{Title: "V"}, {Title: "mV"}}, // the last two channels are in millivolts
		Label: "Four waves", Note: "four channels of a million samples"}
	for s := range 4 {
		ch.Series = append(ch.Series, &plot.Series{
			Name: fmt.Sprintf("channel %d", s+1),
			Y:    channel(s),
			X0:   0,
			Dx:   1.0 / rate,
			Axis: s / 2, // channels 1-2 left, 3-4 right
		})
	}
	if !*live { // notes of the static minute's spikes; a live window moves past them
		ch.Notes = []plot.Note{
			{X: 4.9996, Y: 1.32, Text: "spike"},
			{X: 0, Y: 0.5, Text: "start"},
		}
	}
	streaming := *live || runtime.GOOS == "android"
	if streaming {
		// A window of a second, pinned to the newest samples, fed from
		// another goroutine.
		ch.Follow = time.Second
		ch.Note = "four channels at 20 kHz, the last second"
		for _, s := range ch.Series {
			s.Y = nil
		}
		go feed(ch)
	}

	if *probeOn || runtime.GOOS == "android" {
		probe.On("plot") // the plot/update and plot/paint spans: a prefix, no slash
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
				if streaming {
					c.AnimationFrame() // new samples arrive between frames
				}
				ch.Measure = measure
				if showBand {
					ch.Bands = band
				} else {
					ch.Bands = nil
				}
				// Keep clear of the status bar and the navigation bar.
				a := c.SafeArea()
				ui.Column(c).Fill().Padding(a.Top+8, 8, a.Bottom+8, 8).Gap(8).Children(func() {
					toolbar(c, ch)
					ch.Element(c)
				})
			})})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
