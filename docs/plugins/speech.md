# Speech

The speech plugin says text with the device's own voice: TextToSpeech on
Android, which mygo build compiles into the app. It suits an app that reads a
page aloud, or tells what happened while the user was looking elsewhere.

```go
import "github.com/egoist/mygo/plugins/speech"

err := speech.Speak(ctx, "Your order shipped.", speech.Options{Language: "en"})
```

| Function | What it does |
| --- | --- |
| `Available(ctx)` | Whether the device has a voice to say text with. |
| `Speak(ctx, text, Options)` | Says text, replacing what the voice was saying. It returns once the engine took the text, not when the voice is done. |
| `Stop(ctx)` | Ends what the voice is saying, if it is saying anything. |

`Options` are `Rate` (1 the voice's own, 0.5 half as fast, 2 twice), `Pitch`
(1 the voice's own) and `Language` (a BCP-47 tag, `en`, `pt-BR`); a zero value
leaves the engine's setting, which is what a first call does.

Android 11 and later ask which service an app reaches for text to speech: the
plugin's manifest brings the `<queries>` entry, and the plugin adds no
permission. A device with no engine reports `Available` false, and `Speak`
then fails with a `*mygo.NativeError` whose code is `Unavailable`.

iOS has no Swift for it yet.
