package ui

import "testing"

// ClearFocus, from the view, takes the keyboard from the field that has it
// once the frame is built: what an app does after a form is submitted.
func TestClearFocus(t *testing.T) {
	var clear bool
	tt := coreNewTester(func(c *context) {
		if clear {
			c.rt.public.ClearFocus()
		}
		coreBox(c).Fill().Focusable().Key("field")
	}, 200, 200)
	tt.ClickAt(50, 50)
	tt.Frame()
	if tt.rt.focused == 0 {
		t.Fatal("the press did not focus the field")
	}
	clear = true
	tt.Frame()
	if tt.rt.focused != 0 {
		t.Fatal("ClearFocus kept the focus")
	}
	// It does nothing when nothing has the focus.
	tt.Frame()
	if tt.rt.focused != 0 {
		t.Fatal("an empty focus changed")
	}
}
