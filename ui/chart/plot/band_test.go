package plot

import (
	"image"
	imgcolor "image/color"
	"testing"

	"github.com/egoist/mygo/ui"
)

// pixelAt returns the pixel at (x, y) of an image, or nothing outside it.
func pixelAt(img *image.RGBA, x, y int) imgcolor.RGBA {
	if x < 0 || y < 0 || x >= img.Rect.Dx() || y >= img.Rect.Dy() {
		return imgcolor.RGBA{}
	}
	i := y*img.Stride + x*4
	return imgcolor.RGBA{img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]}
}

func bandChart(t *testing.T) (*Chart, *ui.Tester) {
	t.Helper()
	ch := &Chart{Height: 120, Axes: []Axis{{}, {}},
		Series: []*Series{
			{Y: []float32{0, 1}, X0: 0, Dx: 1},
			{Y: []float32{0, 1000}, X0: 0, Dx: 1, Axis: 1},
		},
		Bands: []Band{
			{Lo: 0.4, Hi: 0.6, Color: ui.RGB(255, 0, 0)},
			{Lo: 0, Hi: 500, Axis: 1, Color: ui.RGB(0, 200, 0)},
		}}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Fill().Background(ui.RGB(255, 255, 255)).Children(func() { ch.Element(c) })
	}, 400, 300)
	tt.Frame()
	return ch, tt
}

func TestBandShadesItsRange(t *testing.T) {
	ch, tt := bandChart(t)
	img := tt.Image()
	if img == nil {
		t.Skip("no image in this build")
	}
	x := int(ch.r.X + ch.r.W*0.3) // clear of the lines, which cross at 0.4
	mid := pixelAt(img, x, int(ch.r.Y+ch.r.H/2))
	if mid.R < 200 || mid.G > 80 {
		t.Fatalf("the middle of the plot is %+v, want the red band", mid)
	}
	top := pixelAt(img, x, int(ch.r.Y+3))
	if top.R < 200 || top.G < 200 {
		t.Fatalf("the top of the plot is %+v, want white", top)
	}
}

func TestBandOnTheSecondAxis(t *testing.T) {
	ch, tt := bandChart(t)
	img := tt.Image()
	if img == nil {
		t.Skip("no image in this build")
	}
	x := int(ch.r.X + ch.r.W*0.75) // clear of the left axis' labels
	// Axis 1 fits 0..1000 out to -500..1500, so 0..500 sits at 25%..50% up.
	low := pixelAt(img, x, int(ch.r.Y+ch.r.H*0.65))
	if low.G < 150 || low.R > 120 {
		t.Fatalf("the lower part is %+v, want the green band", low)
	}
	high := pixelAt(img, x, int(ch.r.Y+ch.r.H*0.1))
	if high.R < 200 || high.G < 200 {
		t.Fatalf("the upper part is %+v, want white", high)
	}
}
