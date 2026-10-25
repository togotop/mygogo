package plot

import (
	"math"

	"github.com/egoist/mygo/ui"
)

// dragState is a press in progress on the plot: a drag selects a range to
// zoom to, and a drag with Shift pans.
type dragState struct {
	active bool
	pan    bool
	fromX  float64 // the data x at the press
	toX    float64 // the data x now
	view   Range   // the view at the press
}

// input takes the plot's input: a drag to zoom to the range it selects,
// Shift and a drag to pan, a double click to fit, Ctrl or Cmd with the
// wheel to zoom under the pointer, and a pinch. The wheel without the
// modifier goes on to the scroll containers around the chart.
func (ch *Chart) input(ev ui.InputEvent) bool {
	if !ch.vx.Valid() || ch.r.W <= 0 {
		return false
	}
	x := ch.xAt(float64(ev.X))
	switch ev.Kind {
	case ui.InputScroll:
		if ev.Mods&(ui.Ctrl|ui.Super) == 0 || ev.Momentum {
			return false
		}
		step := float64(ev.DY)
		if ev.Precise {
			step /= 4
		}
		// One notch of 100 DIPs zooms by a quarter, toward the pointer.
		ch.View = Viewport{X: ch.vx}.ZoomX(math.Pow(1.25, step/100), x)
		return true
	case ui.InputZoom:
		if ev.Scale <= 0 {
			return false
		}
		ch.View = Viewport{X: ch.vx}.ZoomX(1/float64(ev.Scale), x)
		return true
	case ui.InputPointerDown:
		if ev.Button != 0 {
			return false
		}
		if ev.Clicks >= 2 {
			ch.View = Viewport{} // the whole data again
			return true
		}
		ch.drag = dragState{active: true, pan: ev.Mods&ui.Shift != 0, fromX: x, toX: x, view: ch.vx}
		return true
	case ui.InputPointerMove:
		if !ch.drag.active {
			return false
		}
		ch.drag.toX = x
		if ch.drag.pan {
			ch.View = Viewport{X: ch.drag.view}.PanX(ch.drag.fromX - x)
		}
		return true
	case ui.InputPointerUp:
		if !ch.drag.active {
			return false
		}
		d := ch.drag
		ch.drag.active = false
		if d.pan {
			return true
		}
		lo, hi := min(d.fromX, d.toX), max(d.fromX, d.toX)
		if hi-lo > 0.02*ch.vx.Span() { // a click is not a range
			ch.View = Viewport{X: Range{lo, hi}}
		}
		return true
	case ui.InputPointerCancel:
		ch.drag.active = false
		return true
	}
	return false
}

// xAt turns a local x of the plot into a data x.
func (ch *Chart) xAt(local float64) float64 {
	return ch.vx.Lo + local/float64(ch.r.W)*ch.vx.Span()
}

// keys moves and zooms the view from the keyboard, while the plot has the
// focus: the arrows pan, + and - zoom around the middle, Home and Escape
// fit the data again.
func (ch *Chart) keys(area ui.Element) {
	if !ch.vx.Valid() {
		return
	}
	step := ch.vx.Span() * 0.1
	mid := (ch.vx.Lo + ch.vx.Hi) / 2
	switch {
	case area.Shortcut(0, ui.KeyLeft):
		ch.View = Viewport{X: ch.vx}.PanX(-step)
	case area.Shortcut(0, ui.KeyRight):
		ch.View = Viewport{X: ch.vx}.PanX(step)
	case area.Shortcut(0, ui.KeyEqual):
		ch.View = Viewport{X: ch.vx}.ZoomX(0.8, mid)
	case area.Shortcut(0, ui.KeyMinus):
		ch.View = Viewport{X: ch.vx}.ZoomX(1.25, mid)
	case area.Shortcut(0, ui.KeyHome), area.Shortcut(0, ui.KeyEscape):
		ch.View = Viewport{}
	}
}
