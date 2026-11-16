package ui

import "testing"

// Undo takes back the edits a text input took, and Redo puts them back, with
// the bound value following each, as typing does.
//
// An Element names its control in the pass that builds it (Element.lookup),
// so a button's handler asks the element it is built beside, in that pass,
// as a toolbar's Undo button does.
func TestElementUndoRedo(t *testing.T) {
	s := "cafe"
	var canUndo, canRedo bool
	var el Element
	tt := NewTester(func(c *Context) {
		el = TextInput(c, &s).Fill().AutoFocus()
		canUndo, canRedo = el.CanUndo(), el.CanRedo()
		if Button(c, "Undo").KeepFocus().Disabled(!canUndo).Clicked() {
			el.Undo()
		}
		if Button(c, "Redo").KeepFocus().Disabled(!canRedo).Clicked() {
			el.Redo()
		}
	}, 300, 200)
	tt.Frame()

	if canUndo || canRedo {
		t.Fatalf("a fresh input has undo %v, redo %v", canUndo, canRedo)
	}
	if err := tt.Click("Undo"); err != nil {
		t.Logf("Undo is disabled with nothing to take back: %v", err)
	}
	tt.Frame()
	if s != "cafe" {
		t.Fatalf("a disabled Undo changed the text: %q", s)
	}

	// Two edits, a second apart so they do not coalesce into one step.
	tt.Type("d")
	tt.Advance(2e9)
	tt.Type("e")
	if s != "cafede" {
		t.Fatalf("after typing: %q", s)
	}
	tt.Frame()
	if !canUndo {
		t.Fatal("typing leaves nothing to undo")
	}

	if err := tt.Click("Undo"); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	tt.Frame()
	if s != "cafed" {
		t.Fatalf("after one Undo: %q", s)
	}
	if !canRedo {
		t.Fatal("an undone edit cannot be put back")
	}

	if err := tt.Click("Redo"); err != nil {
		t.Fatalf("Redo: %v", err)
	}
	tt.Frame()
	if s != "cafede" {
		t.Fatalf("after Redo: %q", s)
	}
	if canRedo {
		t.Error("Redo is offered with nothing to put back")
	}
}