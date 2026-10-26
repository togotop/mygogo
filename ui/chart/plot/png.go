package plot

import (
	"bytes"
	"errors"
	"fmt"
	"image/png"

	"github.com/egoist/mygo/ui"
)

// PNG renders the chart on its own, as a window of width by height pixels
// would draw it, and returns the image's PNG bytes: a picture to save, paste
// or print. Call it on the main thread; what the chart shows is what it
// exports, so set the view first.
//
// The image has the theme's background, the legend row if the chart has one,
// and as much of the plot as Height leaves room for.
func (ch *Chart) PNG(width, height int) ([]byte, error) {
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("plot: an image of %dx%d pixels", width, height)
	}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Fill().Background(c.Theme().Background).Children(func() { ch.Element(c) })
	}, width, height)
	tt.Frame() // the plot measures its width
	tt.Frame() // and decimates at it
	img := tt.Image()
	if img == nil {
		return nil, errors.New("plot: the renderer made no image")
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
