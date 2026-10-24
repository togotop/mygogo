//go:build android && cgo

package backend

// platform.Power on a phone: keep the screen (or the CPU) awake while something
// needs it, and report whether the device is on battery.
//
// The platform contract is a lease: KeepAwake returns the function that ends it.
// The lease is ended once, even when the caller releases it from several paths
// (retty calls it from render and from lifecycle changes), because a second
// removal would cancel another holder's reason on the Java side.

import (
	"sync"
	"time"

	"github.com/egoist/mygo/internal/platform"
)

// Power returns the phone's power control.
func (b *Backend) Power() platform.Power { return androidPower{b: b} }

type androidPower struct{ b *Backend }

// Watch has nothing to report on a phone this round: there is no desktop
// presence to follow, so no PowerEvent is sent.
func (androidPower) Watch() {}

// OnBattery reads the battery state, not the charger's presence: a device that
// is plugged in but not charging (a full battery, or a weak charger) is still
// on its battery.
func (androidPower) OnBattery() bool {
	bridge := platformBridge()
	if bridge == nil {
		return false
	}
	return bridge.OnBattery()
}

// IdleTime is unknown on a phone; the caller that asks is the desktop's
// presence check, which does not run here. Zero says "no idle information".
func (androidPower) IdleTime() time.Duration { return 0 }

// KeepAwake holds the wake reason until the returned function runs.
func (androidPower) KeepAwake(display bool, reason string) func() {
	bridge := platformBridge()
	if bridge == nil {
		return func() {}
	}
	if err := bridge.KeepAwake(display, reason, true); err != nil {
		return func() {}
	}
	var once sync.Once
	return func() {
		once.Do(func() { _ = bridge.KeepAwake(display, reason, false) })
	}
}
