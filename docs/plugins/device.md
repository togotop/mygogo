# Device

The device plugin tells what the app runs on: the maker and model of the
hardware, and the system's name and version. An app that prints a bug report,
or adapts to a maker's quirks, asks it.

```go
import "github.com/egoist/mygo/plugins/device"

info, err := device.Current(ctx)
if err != nil {
	return err
}
log.Printf("%s %s, %s %s (API %d)", info.Manufacturer, info.Model, info.System, info.SystemVersion, info.SDK)
```

| Function | What it does |
| --- | --- |
| `Current(ctx)` | What the app runs on. |

An `Info` has `Manufacturer` (`Google`, `Samsung`; a desktop may leave it
empty), `Model` (`Pixel 8`, `iPhone15,2`), `System` (`Android`, `iOS`),
`SystemVersion` (`14`, `17.5`) and `SDK`, the API level on Android, and 0
elsewhere.

The plugin brings no permission, and reads what the system tells every app.
iOS has no Swift for it yet.
