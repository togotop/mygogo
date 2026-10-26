package plot

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func noteChart(t *testing.T) (*Chart, *ui.Tester) {
	t.Helper()
	y := make([]float32, 101)
	for i := range y {
		y[i] = float32(i)
	}
	ch := &Chart{Height: 200,
		Series: []*Series{{Y: y, X0: 0, Dx: 1}},
		Notes: []Note{
			{X: 50, Y: 50, Text: "mid"},
			{X: 95, Y: 95, Text: "late"},
		}}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Fill().Children(func() { ch.Element(c) })
	}, 500, 300)
	tt.Frame()
	return ch, tt
}

func TestNotesAreDrawn(t *testing.T) {
	_, tt := noteChart(t)
	if !tt.HasText("mid") || !tt.HasText("late") {
		t.Fatalf("no notes: %v", tt.Texts())
	}
}

func TestNoteOutsideTheViewIsNotDrawn(t *testing.T) {
	ch, tt := noteChart(t)
	ch.View = Viewport{X: Range{0, 40}} // the notes at 50 and 95 are out of view
	tt.Frame()
	if tt.HasText("mid") || tt.HasText("late") {
		t.Fatalf("notes of another part of the data: %v", tt.Texts())
	}
}

func TestNoteOnTheSecondAxis(t *testing.T) {
	big := make([]float32, 2)
	big[0], big[1] = 0, 1000
	ch := &Chart{Height: 200, Axes: []Axis{{}, {}},
		Series: []*Series{{Y: []float32{0, 1}, X0: 0, Dx: 1}, {Y: big, X0: 0, Dx: 1, Axis: 1}},
		Notes: []Note{
			{X: .5, Y: 800, Axis: 1, Text: "right"},
			{X: .5, Y: 800, Axis: 0, Text: "left"},
		}}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Fill().Children(func() { ch.Element(c) })
	}, 500, 300)
	tt.Frame()
	if !tt.HasText("right") {
		t.Fatalf("the note on the second axis is missing: %v", tt.Texts())
	}
	if tt.HasText("left") {
		t.Fatalf("a note off its axis was drawn: %v", tt.Texts())
	}
}
