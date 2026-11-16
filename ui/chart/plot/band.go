package plot

import (
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/chart"
)

// drawBands shades the bands' ranges across the plot, under the series.
func (ch *Chart) drawBands(p *ui.Painter, r ui.Rect) {
	for _, b := range ch.Bands {
		k := 0
		if b.Axis > 0 && b.Axis < ch.axisCount() {
			k = b.Axis
		}
		lo, hi := ch.yf(float32(b.Lo), k), ch.yf(float32(b.Hi), k)
		if lo > hi {
			lo, hi = hi, lo
		}
		lo, hi = max(0, min(1, lo)), max(0, min(1, hi))
		if hi <= lo {
			continue
		}
		col := b.Color
		if col.A == 0 {
			col = ch.tint
		}
		y := chart.Y(r, hi)
		p.Fill(ui.Rect{X: r.X, Y: y, W: r.W, H: (hi - lo) * r.H}, col, 0)
		if b.Label != "" {
			p.Text(r.X+6, y+12, b.Label, 10, ch.mark)
		}
	}
}
