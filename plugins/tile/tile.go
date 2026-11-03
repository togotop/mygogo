// Package tile is the app's tile in the quick settings of Android, which a
// pull down from the top shows: its label, its subtitle and whether it looks
// active. Pressing it comes back as an event.
//
//	err := tile.Set(ctx, tile.Tile{Label: "Desk lamp", Active: on})
//	off := tile.OnClick(func() { toggle() })
//
// Elsewhere its functions report errors.ErrUnsupported.
package tile

import (
	"context"

	"github.com/egoist/mygo"
)

const pluginID = "tile"

// Tile is what the app's tile shows.
type Tile struct {
	// Label is its name, one line.
	Label string
	// Subtitle is the line under it, which Android shows when the tile is
	// wide.
	Subtitle string
	// Active draws the tile as on, and Unavailable greys it out and ignores
	// presses.
	Active      bool
	Unavailable bool
}

// Set replaces what the tile shows. It does nothing where there is no tile.
func Set(ctx context.Context, t Tile) error {
	return mygo.Native(pluginID).Call(ctx, "set", t, nil)
}

// OnClick hears the tile's presses, and reports whether the device has one.
// What the handler does goes through the app's own calls, as tile.Set.
func OnClick(fn func()) (off func()) {
	return mygo.Native(pluginID).On("click", func(data []byte) { fn() })
}
