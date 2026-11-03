# NFC

The NFC plugin reads the tags a phone is held against, writes one, and speaks a
tag's own protocol with it: reader mode with the system's sound and vibration
on Android, where the plugin's Kotlin is what mygo build compiles into the app.

```go
import "github.com/egoist/mygo/plugins/nfc"

scan, err := nfc.Scan(ctx, func(t nfc.Tag) {
	for _, r := range t.Records {
		log.Println(r.Kind, r.Text, r.URI)
	}
})
if err != nil {
	return err
}
defer scan.Stop()
```

| Function | What it does |
| --- | --- |
| `Available(ctx)` | Whether the device has an NFC radio that is on. |
| `Scan(ctx, fn)` | Reports each tag held against the phone, until `Stop`. |
| `WriteText(ctx, text)` | Writes text to the tag last seen, formatting it if it is not yet. |
| `Transceive(ctx, tech, data)` | Sends bytes over one of the tag's technologies (`IsoDep`, `NfcA`, `NfcB`, `NfcF`, `NfcV`) and returns its answer. |

A tag carries `ID` (its UID, hex), `Techs` (the technologies it answers on) and
`Records`: text, URI, MIME and raw records, in the order the tag holds them.
`ErrNoTag` is `WriteText` or `Transceive` with no tag seen yet: it is called
while the phone is held against one.

The plugin declares the `NFC` permission. iOS has no Swift for it yet.
