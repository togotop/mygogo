package ui

import (
	"testing"
	"time"

	"github.com/egoist/mygo/internal/platform"
)

// A finger held still on an element that takes text itself starts a
// selection, whatever the element's own handler does with it: it hears
// InputLongPress, then the moves and the release, and the keyboard does not
// open, as a terminal's selection needs.
func TestLongPressCapturesSelectionWithoutKeyboard(t *testing.T) {
	var events []InputEvent
	longs := func() int {
		n := 0
		for _, ev := range events {
			if ev.Kind == InputLongPress {
				n++
			}
		}
		return n
	}
	tt := coreNewTester(func(c *context) {
		coreBox(c).Fill().Focusable().TouchSelection().TextCaret(Rect{W: 1, H: 20}).HandleInput(func(ev InputEvent) bool {
			events = append(events, ev)
			return true
		})
	}, 320, 240)
	tt.TouchDown(120, 100)
	tt.Advance(499 * time.Millisecond)
	tt.Frame()
	if longs() != 0 || tt.rt.ime.state.Active {
		t.Fatalf("the hold selected early, or opened the keyboard: %+v", events)
	}
	tt.Advance(2 * time.Millisecond)
	tt.Frame()
	if longs() != 1 || tt.rt.ime.state.Active {
		t.Fatalf("the long press was not captured: %+v", events)
	}
	n := len(events)
	tt.TouchMove(220, 150)
	tt.TouchUp(220, 150)
	if got := events[n:]; len(got) != 2 || got[0].Kind != InputPointerMove || got[1].Kind != InputPointerUp {
		t.Fatalf("the selection became a scroll: %+v", got)
	}
	if tt.rt.ime.state.Active || tt.rt.pressed != nil || tt.rt.touch.down {
		t.Fatal("the selection kept the contact or the keyboard")
	}
}

// Moving, canceling, losing the focus or resizing before the hold ends it: a
// finger that scrolls never selects.
func TestScrollAndCancelCannotBecomeLongPress(t *testing.T) {
	for _, cancel := range []platform.SurfaceEventKind{platform.PointerMove, platform.SurfaceBlur, platform.SurfaceResize} {
		holds := 0
		tt := coreNewTester(func(c *context) {
			coreBox(c).Fill().TouchSelection().HandleInput(func(ev InputEvent) bool {
				if ev.Kind == InputLongPress {
					holds++
				}
				return true
			})
		}, 320, 240)
		tt.TouchDown(120, 180)
		if cancel == platform.PointerMove {
			tt.TouchMove(120, 100)
		} else {
			tt.send(platform.SurfaceEvent{Kind: cancel})
		}
		tt.Advance(time.Second)
		tt.Frame()
		if holds != 0 {
			t.Fatalf("%v became a hold", cancel)
		}
	}
}

// A tap on an element that takes text focuses it as usual: the selection's
// hold is only the finger staying still.
func TestTouchSelectionTapsFocus(t *testing.T) {
	tt := coreNewTester(func(c *context) {
		coreBox(c).Fill().Focusable().TouchSelection().TextCaret(Rect{W: 1, H: 20}).HandleInput(func(ev InputEvent) bool {
			return true
		})
	}, 320, 240)
	tt.TouchDown(120, 100)
	tt.TouchUp(120, 100)
	tt.Frame()
	if tt.rt.focused == 0 {
		t.Fatal("a tap did not focus the element")
	}
}
