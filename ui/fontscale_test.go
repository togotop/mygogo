package ui

import (
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

// Text laid out at font scale 2 is about twice as tall as at 1, while the
// padding around it does not change: the difference between sp and dp.
func TestFontScaleScalesTextOnly(t *testing.T) {
	t.Cleanup(func() { platform.SetFontScale(1) })

	build := func(scale float32) (*node, *node) {
		platform.SetFontScale(scale)
		var box, text *node
		coreNewTester(func(c *context) {
			box = coreRow(c).Padding(10).Children(func() {
				text = coreText(c, "Hello")
			})
		}, 400, 300)
		return box, text
	}

	box1, text1 := build(1)
	box2, text2 := build(2)

	if got, want := text2.h, text1.h*2; got < want*0.9 || got > want*1.1 {
		t.Fatalf("text height at scale 2 is %v, want about %v", got, want)
	}
	if delta := box2.h - box1.h; delta < text1.h*0.9 {
		t.Fatalf("box grew by %v, want about the text's growth %v", delta, text1.h)
	}
}
