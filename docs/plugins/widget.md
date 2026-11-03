# Widget

The widget plugin is the app's widget on Android's home screen: a title, a line
of text, and whether it looks on. A press comes back to Go, as the tile's does.

```go
import "github.com/egoist/mygo/plugins/widget"

err := widget.Set(ctx, widget.Widget{Title: "Desk lamp", Text: "on", Active: on})
off := widget.OnClick(func() { go toggle() })
```

| Function | What it does |
| --- | --- |
| `Set(ctx, Widget)` | Replaces what every copy of the widget shows. |
| `OnClick(fn)` | Hears presses on it, until the returned function. |

The plugin brings a layout and the widget's info as resources of its own, which
`mygo build` puts beside the host's (see `resources` in
[Plugins](../plugins.md)), and declares its `AppWidgetProvider` in the app's
manifest. It adds no permission: the user adds the widget from the home screen,
where it shows what the last `Set` said.

Android only.
