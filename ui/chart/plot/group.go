package plot

// Group shares one x view and one cursor across the charts that name it, as
// uPlot's sync does: a drag in one zooms them all, and the crosshairs line
// up. Each chart still fits its own y range to its own series.
//
//	g := &plot.Group{}
//	top := &plot.Chart{Series: ..., Sync: g}
//	below := &plot.Chart{Series: ..., Sync: g}
type Group struct {
	View Viewport
	// Cursor is the data x under the pointer of whichever chart of the
	// group has it; HasCursor says there is one.
	Cursor    float64
	HasCursor bool
}

// view returns the viewport the chart shows: the group's, when it has one.
func (ch *Chart) view() Viewport {
	if ch.Sync != nil {
		return ch.Sync.View
	}
	return ch.View
}

// setView records a new viewport, in the group when the chart has one.
func (ch *Chart) setView(v Viewport) {
	if ch.Sync != nil {
		ch.Sync.View = v
		return
	}
	ch.View = v
}
