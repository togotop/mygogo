# Scanner

The scanner plugin reads a barcode or a QR code from a photo the app's user
takes with the system's camera app. The app needs no camera permission of its
own, and no preview to draw: the system's camera is a screen the user knows,
and ZXing, compiled into the app, reads the pixels.

```go
import "github.com/egoist/mygo/plugins/scanner"

text, err := scanner.Scan(ctx, scanner.Options{Formats: []string{"qr"}})
switch {
case errors.Is(err, scanner.ErrCanceled):
	return nil // the user went back
case errors.Is(err, scanner.ErrNoCode):
	return errors.New("no code in the photo")
case err != nil:
	return err
}
log.Println(text)
```

| Function | What it does |
| --- | --- |
| `Available(ctx)` | Whether the device has a camera app to take a photo with. |
| `Scan(ctx, Options{Formats})` | Has the user take a photo and returns what its code says. |

`Formats` names what to look for: `qr`, `ean13`, `ean8`, `upca`, `upce`,
`code128`, `code39`, `codabar`, `itf`, `datamatrix`, `pdf417` or `aztec`.
Empty looks for all of them, which takes the longest to read. Call `Scan` off
the UI thread: it waits for the camera.

The photo goes to the app's cache through the host's FileProvider, and is
deleted once read. The plugin adds the `<queries>` entry Android 11 and later
need to see the camera app.

iOS has no Swift for it yet.
