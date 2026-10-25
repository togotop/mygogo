package plot

// Viewport is what the chart shows: a range along x, and one along y.
// A zero Y means auto: the chart fits it to the visible samples.
type Viewport struct {
	X, Y Range
}

// ZoomX scales the view around the x at, f < 1 zooming in, as the wheel
// does under the pointer. It never turns the range inside out or empty.
func (v Viewport) ZoomX(f, at float64) Viewport {
	if f <= 0 || !v.X.Valid() {
		return v
	}
	lo := at - (at-v.X.Lo)*f
	hi := at + (v.X.Hi-at)*f
	if hi-lo < 1e-12 {
		return v
	}
	v.X = Range{lo, hi}
	return v
}

// PanX moves the view by dx data units.
func (v Viewport) PanX(dx float64) Viewport {
	if !v.X.Valid() {
		return v
	}
	v.X = Range{v.X.Lo + dx, v.X.Hi + dx}
	return v
}

// Clamp keeps the view within half a screen of empty space beyond each
// end of the data, as panning past the edge settles.
func (v Viewport) Clamp(data Range) Viewport {
	if !v.X.Valid() || !data.Valid() {
		return v
	}
	span := v.X.Span()
	if span >= data.Span() {
		v.X = data
		return v
	}
	lo := min(max(v.X.Lo, data.Lo-span/2), data.Hi+span/2-span)
	v.X = Range{lo, lo + span}
	return v
}

// Fit returns the view of the whole data range.
func (v Viewport) Fit(data Range) Viewport {
	if data.Valid() {
		v.X = data
	}
	return v
}
