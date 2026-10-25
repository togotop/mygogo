package plot

import (
	"fmt"
	"strconv"

	"github.com/egoist/mygo/ui"
)

// track records where the pointer is over the plot, for the cursor: the
// data x under it, or nothing when it is away or a drag is in progress.
func (ch *Chart) track(area ui.Element) {
	ch.cursorOn = false
	if !ch.vx.Valid() || ch.drag.active {
		return
	}
	x, _, over := area.PointerPosition()
	w := area.Bounds().W
	switch {
	case over && w > 0:
		ch.cursorX = ch.vx.Lo + float64(x/w)*ch.vx.Span()
		ch.cursorOn = true
		ch.owns = true
		if ch.Sync != nil {
			ch.Sync.Cursor, ch.Sync.HasCursor = ch.cursorX, true
		}
	case ch.owns: // it had the pointer, and now it does not
		ch.owns = false
		if ch.Sync != nil {
			ch.Sync.HasCursor = false
		}
	}
	if !ch.cursorOn && ch.Sync != nil && ch.Sync.HasCursor {
		ch.cursorX, ch.cursorOn = ch.Sync.Cursor, true
	}
}

// stamp returns the sample of s nearest the cursor, and whether there is
// one: the value the legend shows.
func (ch *Chart) stamp(s *Series) (float32, bool) {
	if !ch.cursorOn || s.Count() == 0 {
		return 0, false
	}
	i := s.IndexAt(ch.cursorX)
	if i < 0 || i >= s.Count() {
		return 0, false
	}
	v := s.YAt(i)
	if v != v {
		return 0, false
	}
	return v, true
}

// color returns the series' color, or the palette's by index.
func color(i int, s *Series) ui.Color {
	if s.Color.A != 0 {
		return s.Color
	}
	return palette[i%len(palette)]
}

// name returns the series' name, or a number.
func name(i int, s *Series) string {
	if s.Name != "" {
		return s.Name
	}
	return fmt.Sprintf("series %d", i+1)
}

// legendRow shows each series: a swatch of its color, its name, and under
// the cursor the value it has there.
func (ch *Chart) legendRow(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Gap(14).Children(func() {
		for i, s := range ch.Series {
			if s.Hidden {
				continue
			}
			label := name(i, s)
			if v, ok := ch.stamp(s); ok {
				label += "  " + strconv.FormatFloat(float64(v), 'f', 3, 32)
			}
			ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Size(8, 8).Radius(2).Background(color(i, s))
				ui.Text(c, label).NoWrap().FontSize(11).TextColor(t.TextMuted)
			})
		}
	})
}

// describe says what the cursor is on, for assistive technology.
func (ch *Chart) describe() string {
	if !ch.cursorOn {
		return ""
	}
	out := ""
	for i, s := range ch.Series {
		if s.Hidden {
			continue
		}
		if v, ok := ch.stamp(s); ok {
			if out != "" {
				out += ", "
			}
			out += name(i, s) + " " + strconv.FormatFloat(float64(v), 'f', 3, 32)
		}
	}
	return out
}
