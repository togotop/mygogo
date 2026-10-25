package plot

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestAxisTitlesAreDrawn(t *testing.T) {
	small := &Series{Y: []float32{0, 1}, X0: 0, Dx: 1}
	big := &Series{Y: []float32{0, 1000}, X0: 0, Dx: 1, Axis: 1}
	ch := &Chart{Height: 200, Series: []*Series{small, big},
		Axes: []Axis{{Title: "volts"}, {Title: "amps"}}}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Fill().Children(func() { ch.Element(c) })
	}, 500, 300)
	tt.Frame()
	if !tt.HasText("volts") || !tt.HasText("amps") {
		t.Fatalf("no axis titles: %v", tt.Texts())
	}
}
