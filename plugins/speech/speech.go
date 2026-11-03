// Package speech says text with the system's own voice: TextToSpeech on
// Android, AVSpeechSynthesizer on iOS, as an app that reads a page aloud, or
// tells what happened, needs.
//
//	err := speech.Speak(ctx, "Your order shipped.", speech.Options{Language: "en"})
//
// Each Speak replaces what the voice was saying, and Stop ends it. Elsewhere
// its functions report errors.ErrUnsupported.
package speech

import (
	"context"

	"github.com/egoist/mygo"
)

const pluginID = "speech"

// Options are how the voice says text.
type Options struct {
	// Rate is how fast, 1 the voice's own, 0.5 half as fast and 2 twice.
	// Zero leaves the engine's setting.
	Rate float32
	// Pitch is how high, 1 the voice's own. Zero leaves the engine's.
	Pitch float32
	// Language is the BCP-47 tag of the voice, "en", "pt-BR"; empty keeps
	// the one the engine had.
	Language string
}

// Available reports whether the device has a voice to say text with.
func Available(ctx context.Context) (bool, error) {
	var r struct {
		Available bool `json:"available"`
	}
	if err := mygo.Native(pluginID).Call(ctx, "available", nil, &r); err != nil {
		return false, err
	}
	return r.Available, nil
}

// Speak says text, replacing what the voice was saying. It returns once the
// engine took the text, not when the voice is done: the speaking goes on
// after.
func Speak(ctx context.Context, text string, o Options) error {
	args := map[string]any{"Text": text}
	if o.Rate != 0 {
		args["Rate"] = o.Rate
	}
	if o.Pitch != 0 {
		args["Pitch"] = o.Pitch
	}
	if o.Language != "" {
		args["Language"] = o.Language
	}
	return mygo.Native(pluginID).Call(ctx, "speak", args, nil)
}

// Stop ends what the voice is saying, if it is saying anything.
func Stop(ctx context.Context) error {
	return mygo.Native(pluginID).Call(ctx, "stop", nil, nil)
}
