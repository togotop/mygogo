package mygo

import "encoding/json"

// TileContent is what the quick settings tile shows. Active draws it in the
// on state, which is what a tile for a switch or a scene usually means.
type TileContent struct {
	Label    string `json:"label"`
	Subtitle string `json:"subtitle"`
	Active   bool   `json:"active"`
}

// TileModule drives the quick settings tile.
type TileModule struct{}

// Tile is the quick settings tile.
var Tile = &TileModule{}

// Set replaces the tile's label, subtitle and state. A tile the person has not
// added is not an error: the answer says the content was taken, and the tile
// shows it whenever it is added.
func (m *TileModule) Set(content TileContent) error {
	needsApp("Tile.Set")
	encoded, err := json.Marshal(content)
	if err != nil {
		return err
	}
	var updateErr error
	onMain(func() { updateErr = backend().Mobile().UpdateTile(string(encoded)) })
	return updateErr
}

// OnClick receives a tile tap on the UI thread.
func (m *TileModule) OnClick(fn func()) (off func()) {
	return mobileEvents.tileClicks.add(fn, false)
}
