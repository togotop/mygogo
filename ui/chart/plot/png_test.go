package plot

import (
	"bytes"
	"image/png"
	"testing"
)

func TestPNG(t *testing.T) {
	y := make([]float32, 400)
	for i := range y {
		y[i] = float32(.5 + .3*float32(i%7)/7)
	}
	ch := &Chart{Height: 200, Series: []*Series{{Y: y, X0: 0, Dx: .01}}}
	b, err := ch.PNG(400, 300)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 400 || img.Bounds().Dy() != 300 {
		t.Fatalf("image is %v, want 400x300", img.Bounds())
	}
	// The chart is in the image: many pixels differ from its background.
	bg := img.At(1, 1)
	drawn := 0
	for y2 := 0; y2 < 300; y2++ {
		for x := 0; x < 400; x++ {
			if img.At(x, y2) != bg {
				drawn++
			}
		}
	}
	if drawn < 200 {
		t.Fatalf("only %d pixels differ from the corner: the chart is not in the image", drawn)
	}
}

func TestPNGRejectsAnEmptyImage(t *testing.T) {
	ch := &Chart{Series: []*Series{{Y: []float32{1, 2}, X0: 0, Dx: 1}}}
	if _, err := ch.PNG(0, 300); err == nil {
		t.Fatal("no error for a zero width")
	}
	if _, err := ch.PNG(400, -1); err == nil {
		t.Fatal("no error for a negative height")
	}
}
