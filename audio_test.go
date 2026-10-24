//go:build !js

package mygo

import (
	"errors"
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

// TestAudioRejectsBadInput checks the arguments that would otherwise reach the
// platform as a surprising volume or key.
func TestAudioRejectsBadInput(t *testing.T) {
	for _, percent := range []int{-1, 101} {
		if err := Audio.SetVolume(percent); err == nil {
			t.Errorf("SetVolume(%d) was accepted", percent)
		}
	}
	if err := Audio.Press("volume-up"); err == nil {
		t.Error("Press accepted a key that is not a media key")
	}
	if err := Audio.SetRingerMode("loud"); err == nil {
		t.Error("SetRingerMode accepted a mode that does not exist")
	}
	if err := Audio.Adjust(0); err != nil {
		t.Errorf("Adjust(0) = %v, want nil: nothing to do is not an error", err)
	}
}

// TestAudioOnAPlatformWithoutControls checks the desktop answer: the fake
// backend has no media controls, so every call says so rather than silently
// doing nothing.
func TestAudioOnAPlatformWithoutControls(t *testing.T) {
	if err := Audio.SetVolume(50); !errors.Is(err, platform.ErrUnsupported) {
		t.Errorf("SetVolume = %v, want ErrUnsupported", err)
	}
	if err := Audio.SetMuted(true); !errors.Is(err, platform.ErrUnsupported) {
		t.Errorf("SetMuted = %v, want ErrUnsupported", err)
	}
	if err := Audio.Press(MediaPlayPause); !errors.Is(err, platform.ErrUnsupported) {
		t.Errorf("Press = %v, want ErrUnsupported", err)
	}
	if err := Audio.SetRingerMode(RingerVibrate); !errors.Is(err, platform.ErrUnsupported) {
		t.Errorf("SetRingerMode = %v, want ErrUnsupported", err)
	}
	if err := Audio.OpenSilenceSettings(); !errors.Is(err, platform.ErrUnsupported) {
		t.Errorf("OpenSilenceSettings = %v, want ErrUnsupported", err)
	}
	if Audio.SilenceAllowed() {
		t.Error("SilenceAllowed on a platform without controls")
	}
	if got := Audio.Volume(); got != (AudioVolume{}) {
		t.Errorf("Volume = %+v, want zeroes", got)
	}
}
