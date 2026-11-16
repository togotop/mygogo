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
	defer tt.rt.close()
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
// ClearFocus reads a FocusBind field as its zero, so that the field does not
// hold the focus the app just took away: assigning that value again asks for
// it back.
func TestClearFocusWithFocusBind(t *testing.T) {
	type pane int
	const (
		none pane = iota
		first
	)
	desired := first
	clear := false
	var c *Context
	tt := NewTester(func(ctx *Context) {
		c = ctx
		if clear {
			ctx.ClearFocus()
		}
		Button(ctx, "First").FocusBind(&desired, first)
	}, 200, 100)
	tt.Frame()
	if !tt.Focused("First") {
		t.Fatal("the binding did not focus the button")
	}

	clear = true
	tt.Frame()
	if tt.Focused("First") {
		t.Fatal("ClearFocus kept the focus")
	}
	if got := FocusedValue(c, &desired); got != none {
		t.Errorf("the field kept the focus ClearFocus took: %v", got)
	}
	if desired != none {
		t.Errorf("the bound field reads %v, want its zero", desired)
	}

	// Assigning the value again asks for the focus again.
	clear = false
	desired = first
	tt.Frame()
	if !tt.Focused("First") {
		t.Fatal("asking for the focus again did nothing")
	}
}
