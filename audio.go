package mygo

import (
	"fmt"

	"github.com/egoist/mygo/internal/platform"
)

// AudioVolume is the phone's media volume: how loud, whether it is muted, and
// what the ringer is doing.
type AudioVolume struct {
	// Percent is the media volume, 0 to 100.
	Percent int
	Muted   bool
	// Ringer is "normal", "vibrate" or "silent".
	Ringer string
}

// Ringer modes, as Android names them.
const (
	RingerNormal  = "normal"
	RingerVibrate = "vibrate"
	RingerSilent  = "silent"
)

// MediaKey is one transport key: what a remote control sends to whatever is
// playing.
type MediaKey string

const (
	MediaPlayPause MediaKey = "play-pause"
	MediaPlay      MediaKey = "play"
	MediaPause     MediaKey = "pause"
	MediaNext      MediaKey = "next"
	MediaPrevious  MediaKey = "previous"
	MediaStop      MediaKey = "stop"
	MediaForward   MediaKey = "forward"
	MediaRewind    MediaKey = "rewind"
)

// AudioModule is the phone's media volume and transport keys: what a control
// app needs to say "turn it down" or "pause", and what a scene needs to say
// "go silent when the movie starts".
//
// On Android this is AudioManager: the media stream only, so the alarm and the
// ringer keep their own levels. Desktop backends have no such thing and answer
// platform.ErrUnsupported.
type AudioModule struct{}

// Audio is the phone's media volume and transport keys.
var Audio = &AudioModule{}

// ErrDndAccess is the system refusing silent: Android 6 and later want the
// notification policy (do not disturb) access for it.
var ErrDndAccess = platform.ErrDndAccess

// Volume reports the media volume. A platform without media controls answers
// zeroes.
func (a *AudioModule) Volume() AudioVolume {
	needsApp("Audio.Volume")
	var info platform.AudioVolumeInfo
	_ = audioControl(func(c platform.AudioControl) error {
		got, err := c.Volume()
		info = got
		return err
	})
	return AudioVolume{Percent: info.Percent, Muted: info.Muted, Ringer: info.Ringer}
}

// SetVolume sets the media volume, 0 to 100. It moves the media stream, not
// the ringer or the alarm.
func (a *AudioModule) SetVolume(percent int) error {
	needsApp("Audio.SetVolume")
	if percent < 0 || percent > 100 {
		return fmt.Errorf("mygo: volume %d is outside 0..100", percent)
	}
	return audioControl(func(c platform.AudioControl) error {
		return c.SetVolume(jsonPayload(map[string]any{"percent": percent}))
	})
}

// Adjust moves the media volume by steps, which may be negative, one step
// being what a volume key moves.
func (a *AudioModule) Adjust(steps int) error {
	needsApp("Audio.Adjust")
	if steps == 0 {
		return nil
	}
	return audioControl(func(c platform.AudioControl) error {
		return c.SetVolume(jsonPayload(map[string]any{"steps": steps}))
	})
}

// SetMuted mutes or unmutes the media stream.
func (a *AudioModule) SetMuted(on bool) error {
	needsApp("Audio.SetMuted")
	return audioControl(func(c platform.AudioControl) error {
		return c.SetVolume(jsonPayload(map[string]any{"muted": on}))
	})
}

// Press sends one transport key to whatever is playing. It needs no
// permission: the system treats it as a media button.
func (a *AudioModule) Press(key MediaKey) error {
	needsApp("Audio.Press")
	switch key {
	case MediaPlayPause, MediaPlay, MediaPause, MediaNext, MediaPrevious, MediaStop, MediaForward, MediaRewind:
	default:
		return fmt.Errorf("mygo: unknown media key %q", key)
	}
	return audioControl(func(c platform.AudioControl) error {
		return c.MediaKey(jsonPayload(map[string]any{"key": string(key)}))
	})
}

// SetRingerMode sets the ringer: RingerNormal, RingerVibrate or RingerSilent.
// Silent is the one the system guards: without the do-not-disturb access it
// answers ErrDndAccess, and SilenceAllowed reports whether the access is
// there.
func (a *AudioModule) SetRingerMode(mode string) error {
	needsApp("Audio.SetRingerMode")
	switch mode {
	case RingerNormal, RingerVibrate, RingerSilent:
	default:
		return fmt.Errorf("mygo: unknown ringer mode %q", mode)
	}
	return audioControl(func(c platform.AudioControl) error {
		return c.RingerMode(jsonPayload(map[string]any{"mode": mode}))
	})
}

// SilenceAllowed reports whether SetRingerMode(RingerSilent) will work.
func (a *AudioModule) SilenceAllowed() bool {
	needsApp("Audio.SilenceAllowed")
	allowed := false
	_ = audioControl(func(c platform.AudioControl) error {
		allowed = c.SilenceAllowed()
		return nil
	})
	return allowed
}

// OpenSilenceSettings shows the system page where the do-not-disturb access
// is granted. The user grants it; the app cannot.
func (a *AudioModule) OpenSilenceSettings() error {
	needsApp("Audio.OpenSilenceSettings")
	return audioControl(func(c platform.AudioControl) error {
		return c.OpenSilenceSettings()
	})
}

// audioControl runs fn with the platform's media controls, or answers
// ErrUnsupported when it has none.
func audioControl(fn func(platform.AudioControl) error) error {
	var err error
	onMain(func() {
		control, ok := backend().Mobile().(platform.AudioControl)
		if !ok {
			err = platform.ErrUnsupported
			return
		}
		err = fn(control)
	})
	return err
}
