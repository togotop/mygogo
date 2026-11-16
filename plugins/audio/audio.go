// Package audio is the phone's media volume, its ringer and the transport
// keys of whatever is playing: what a control app needs to say "turn it
// down", and what a player needs to say "pause".
//
//	vol, err := audio.Current(ctx)
//	err = audio.Adjust(ctx, -1)   // one press of the volume key
//	err = audio.Set(ctx, 40)      // 40%, on the media stream only
//	err = audio.Press(ctx, audio.KeyPlayPause)
//
// Only the media stream moves: the alarm and the ringer keep their own
// levels, and the ringer is set on its own (RingerSilent needs the system's
// do not disturb access, which the user grants). Elsewhere its functions
// report errors.ErrUnsupported.
package audio

import (
	"context"
	"errors"
	"fmt"

	"github.com/egoist/mygo"
)

const pluginID = "audio"

// Volume is the media volume: how loud, whether the stream is muted, and what
// the ringer is doing.
type Volume struct {
	// Percent is the media volume, 0 to 100.
	Percent int `json:"percent"`
	// Muted is the media stream muted, which is not the same as 0%: a
	// muted stream comes back at the volume it had.
	Muted bool `json:"muted"`
	// Ringer is RingerNormal, RingerVibrate or RingerSilent.
	Ringer string `json:"ringer"`
}

// Ringer modes, as Android names them.
const (
	RingerNormal  = "normal"
	RingerVibrate = "vibrate"
	RingerSilent  = "silent"
)

// ErrDndAccess is the system refusing silent: Android 6 and later want the
// notification policy (do not disturb) access for it, which the user grants
// in the system settings OpenSilenceSettings shows.
var ErrDndAccess = errors.New("audio: the do not disturb access is not granted")

// Key is one transport key: what a remote control sends to whatever is
// playing.
type Key string

const (
	KeyPlayPause Key = "play-pause"
	KeyPlay      Key = "play"
	KeyPause     Key = "pause"
	KeyNext      Key = "next"
	KeyPrevious  Key = "previous"
	KeyStop      Key = "stop"
	KeyForward   Key = "forward"
	KeyRewind    Key = "rewind"
)

// Available reports whether the device has media controls to read and move.
func Available(ctx context.Context) (bool, error) {
	var r struct {
		Available bool `json:"available"`
	}
	if err := mygo.Native(pluginID).Call(ctx, "available", nil, &r); err != nil {
		return false, err
	}
	return r.Available, nil
}

// Current reports the media volume and the ringer.
func Current(ctx context.Context) (Volume, error) {
	var v Volume
	if err := mygo.Native(pluginID).Call(ctx, "current", nil, &v); err != nil {
		return Volume{}, err
	}
	return v, nil
}

// Set sets the media volume, 0 to 100. It moves the media stream, not the
// ringer or the alarm, and unmutes a muted stream.
func Set(ctx context.Context, percent int) error {
	if percent < 0 || percent > 100 {
		return fmt.Errorf("audio: volume %d is outside 0..100", percent)
	}
	return mygo.Native(pluginID).Call(ctx, "set", map[string]int{"percent": percent}, nil)
}

// Adjust moves the media volume by steps, which may be negative, one step
// being what a volume key moves.
func Adjust(ctx context.Context, steps int) error {
	if steps == 0 {
		return nil
	}
	return mygo.Native(pluginID).Call(ctx, "adjust", map[string]int{"steps": steps}, nil)
}

// SetMuted mutes or unmutes the media stream.
func SetMuted(ctx context.Context, on bool) error {
	return mygo.Native(pluginID).Call(ctx, "mute", map[string]bool{"on": on}, nil)
}

// SetRinger sets the ringer: RingerNormal, RingerVibrate or RingerSilent.
// Silent is the one the system guards: without the do not disturb access it
// answers ErrDndAccess, and SilenceAllowed reports whether the access is
// there.
func SetRinger(ctx context.Context, mode string) error {
	switch mode {
	case RingerNormal, RingerVibrate, RingerSilent:
	default:
		return fmt.Errorf("audio: unknown ringer mode %q", mode)
	}
	err := mygo.Native(pluginID).Call(ctx, "ringer", map[string]string{"mode": mode}, nil)
	var n *mygo.NativeError
	if errors.As(err, &n) && n.Code == "DndAccess" {
		return ErrDndAccess
	}
	return err
}

// SilenceAllowed reports whether SetRinger(RingerSilent) will work: the user
// granted the do not disturb access, which an app cannot grant itself.
func SilenceAllowed(ctx context.Context) (bool, error) {
	var r struct {
		Allowed bool `json:"allowed"`
	}
	if err := mygo.Native(pluginID).Call(ctx, "silenceAllowed", nil, &r); err != nil {
		return false, err
	}
	return r.Allowed, nil
}

// OpenSilenceSettings shows the system page where the do not disturb access
// is granted. The user grants it; the app cannot.
func OpenSilenceSettings(ctx context.Context) error {
	return mygo.Native(pluginID).Call(ctx, "openSilenceSettings", nil, nil)
}

// Press sends one transport key to whatever is playing. It needs no
// permission: the system treats it as a media button.
func Press(ctx context.Context, key Key) error {
	switch key {
	case KeyPlayPause, KeyPlay, KeyPause, KeyNext, KeyPrevious, KeyStop, KeyForward, KeyRewind:
	default:
		return fmt.Errorf("audio: unknown media key %q", key)
	}
	return mygo.Native(pluginID).Call(ctx, "press", map[string]string{"key": string(key)}, nil)
}
