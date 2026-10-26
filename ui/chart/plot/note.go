package plot

import (
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/chart"
)

// Note is a label at a point of the data, as "trigger" or "sensor swapped":
// a dot on the point and its text to the right of it, which pans and zooms
// with the data. A note outside the view is not drawn, and the text is not
// placed for you: keep a note a little clear of the right edge.
type Note struct {
	X, Y  float64
	Axis  int      // the y axis Y is on; 0 when out of range
	Text  string
	Color ui.Color // zero: the theme's accent and muted text
}

// notes draws the notes of this frame, above the series.
func (ch *Chart) notes(c *ui.Context) {
	if len(ch.Notes) == 0 || !ch.vx.Valid() {
		return
	}
	accent := c.Theme().Accent
	for _, n := range ch.Notes {
		if n.X < ch.vx.Lo || n.X > ch.vx.Hi {
			continue
		}
		k := 0
		if n.Axis > 0 && n.Axis < ch.axisCount() {
			k = n.Axis
		}
		g := ch.yf(float32(n.Y), k)
		if g < 0 || g > 1 {
			continue
		}
		f := float32((n.X - ch.vx.Lo) / ch.vx.Span())
		dot, text := accent, ch.mark
		if n.Color.A != 0 {
			dot, text = n.Color, n.Color
		}
		chart.Anchor(c, f, g, ui.Start, ui.Center, func() {
			ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Size(5, 5).Radius(3).Background(dot)
				ui.Text(c, n.Text).FontSize(10).NoWrap().TextColor(text)
			})
		})
	}
}
