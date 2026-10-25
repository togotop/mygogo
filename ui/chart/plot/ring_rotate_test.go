package plot

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// Rotating a ring in place—Head moves, nothing else does—must decimate again:
// the samples under the same x are different ones.
func TestRingRotationIsPickedUp(t *testing.T) {
	y := []float32{1, 2, 3, 4, 5, 6, 7, 8}
	s := &Series{Y: y, X0: 0, Dx: 1}
	ch := &Chart{Height: 120, Series: []*Series{s}}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Fill().Children(func() { ch.Element(c) })
	}, 400, 300)
	tt.Frame()
	first := ch.cache[0].lo[0]
	if first != 1 {
		t.Fatalf("first column starts at %g, want 1", first)
	}
	s.Head = 3 // the ring rotated: the oldest sample is now 4
	tt.Frame()
	if got := ch.cache[0].lo[0]; got != 4 {
		t.Fatalf("first column starts at %g after the rotation, want 4", got)
	}
}
