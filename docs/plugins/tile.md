# Tile

The tile plugin is the app's tile in Android's quick settings, which a pull
down from the top of the screen shows: a label, a line under it, and whether it
looks on. Pressing it comes back as an event, so a tile can switch a lamp or
start a recording without opening the app.

```go
import "github.com/egoist/mygo/plugins/tile"

err := tile.Set(ctx, tile.Tile{Label: "Desk lamp", Subtitle: "on", Active: on})
off := tile.OnClick(func() { go toggle() })
```

| Function | What it does |
| --- | --- |
| `Set(ctx, Tile)` | Replaces what the tile shows: `Label`, `Subtitle`, and `Active` or `Unavailable`, which greys it out and ignores presses. |
| `OnClick(fn)` | Hears the tile's presses, until the returned function. |

The tile keeps what it last showed, so a tile the user adds later starts there.
The plugin declares its `TileService` in the app's manifest, through its
`mygo-plugin.json`, and draws its own icon; it adds no permission, and Android
asks the user whether to keep the tile when it is first set.

Android only: iOS has no such surface, and a desktop has none either.
