package plot

import (
	"math"
	"testing"
)

func TestZoomKeepsThePointUnderThePointer(t *testing.T) {
	v := Viewport{X: Range{0, 100}}
	at := 30.0
	z := v.ZoomX(0.5, at)
	if math.Abs(z.X.Lo-15) > 1e-9 || math.Abs(z.X.Hi-65) > 1e-9 {
		t.Fatalf("ZoomX = %+v", z.X)
	}
	if f := (at - z.X.Lo) / z.X.Span(); math.Abs(f-0.3) > 1e-9 {
		t.Fatalf("point moved to %g", f)
	}
}

func TestZoomNeverCollapses(t *testing.T) {
	v := Viewport{X: Range{0, 100}}
	if z := v.ZoomX(1e-9, 50); z.X.Span() <= 0 {
		t.Fatalf("span %g", z.X.Span())
	}
	if z := v.ZoomX(0, 50); z != v {
		t.Fatal("zoom by 0 changed the view")
	}
}

func TestPanKeepsTheSpan(t *testing.T) {
	v := Viewport{X: Range{10, 20}}
	if p := v.PanX(-5); p.X != (Range{5, 15}) {
		t.Fatalf("PanX = %+v", p.X)
	}
}

func TestClampLeavesHalfAScreen(t *testing.T) {
	data := Range{0, 100}
	v := Viewport{X: Range{80, 130}}.Clamp(data)
	if v.X != (Range{75, 125}) {
		t.Fatalf("Clamp = %+v, want 75..125", v.X)
	}
	if w := (Viewport{X: Range{-40, 10}}).Clamp(data); w.X != (Range{-25, 25}) {
		t.Fatalf("Clamp left = %+v", w.X)
	}
	if w := (Viewport{X: Range{0, 400}}).Clamp(data); w.X != data {
		t.Fatalf("Clamp wide = %+v", w.X)
	}
}
