// Package widget is the app's widget on the home screen of Android: a title, a
// line of text, and whether it looks active.
//
//	err := widget.Set(ctx, widget.Widget{Title: "Desk lamp", Text: "on", Active: on})
//	off := widget.OnClick(func() { toggle() })
//
// Elsewhere its functions report errors.ErrUnsupported.
package widget

import (
	"context"

	"github.com/egoist/mygo"
)

const pluginID = "widget"

// Widget is what the app's widget shows.
type Widget struct {
	// Title is the bold line, and Text the line under it.
	Title, Text string
	// Active draws the widget as on.
	Active bool
}

// Set replaces what the widget shows, in every copy of it on the home screen.
// A copy added later shows what the last Set said.
func Set(ctx context.Context, w Widget) error {
	return mygo.Native(pluginID).Call(ctx, "set", w, nil)
}

// OnClick hears presses on the widget, and reports whether the device has one.
func OnClick(fn func()) (off func()) {
	return mygo.Native(pluginID).On("click", func(data []byte) { fn() })
}
