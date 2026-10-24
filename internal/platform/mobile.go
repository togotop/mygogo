package platform

import (
	"errors"
	"os"
	"runtime"
	"time"
)

// Mobile translates device services. Methods and callbacks run on the UI thread.
type Mobile interface {
	Haptic(kind string) error
	SetBadge(count int, done func(error))
	SetStatusBar(style string, hidden bool) error
	RegisterPush() error
	Device() DeviceInfo
	// DismissKeyboard ends text editing in the application's windows.
	DismissKeyboard()
	// ScanCode presents a camera scanner for a QR code. done runs exactly once;
	// cancel dismisses the scanner, completing with ErrScanCanceled.
	ScanCode(o ScanOptions, done func(string, error)) (cancel func())
	// PrepareNetwork makes one request to url, letting a system that gates
	// network access ask for it before raw sockets are opened. done runs
	// exactly once; cancel stops the request.
	PrepareNetwork(url string, timeout time.Duration, done func(error)) (cancel func())
	// SetBackHandler installs fn, which the system's back gesture calls on
	// the thread that delivers it. fn reports whether the app went back a
	// page, and the system leaves the app when it did not; it must return
	// quickly and only read state, as the work it starts goes through
	// RunOnMain. Platforms without the gesture ignore it.
	SetBackHandler(fn func() bool)
	// KeepRunning asks the system to let the app keep working in the
	// background, showing reason while it does. The returned stop ends it,
	// once, and is nil only when the answer is an error. Platforms that cannot
	// grant this return ErrUnsupported.
	KeepRunning(reason string) (stop func(), err error)
	// Location is the last position the system recorded. ErrUnsupported means
	// the platform has no location at all; ErrPermission means the app may not
	// read it yet. Platforms without a sensor return ErrUnsupported.
	Location() (Position, error)
	// UpdateWidget replaces what the home screen widget shows, from the JSON
	// the app describes. Platforms without widgets return ErrUnsupported.
	UpdateWidget(json string) error
	// BatteryExempt reports whether the system lets the app run without its
	// background limits. Android asks the user for that exemption; platforms
	// that do not limit apps report true.
	BatteryExempt() (bool, error)
	// SetSecureScreen keeps the window's content out of screenshots, screen
	// recordings and the recents thumbnail. errUnsupported where the platform
	// has no such switch.
	SetSecureScreen(on bool) error
	// UpdateTile replaces what the quick settings tile shows, from the JSON
	// the app describes. Platforms without one return ErrUnsupported.
	UpdateTile(json string) error
	// Speak says text with the system's speech synthesis. Starting the engine
	// takes a moment: errSpeechStarting says to try again, ErrUnsupported that
	// the device has no engine.
	Speak(text string, rate, pitch float64, flush bool) error
	// StopSpeaking stops whatever synthesis is playing.
	StopSpeaking() error
	// StartBLEScan starts a scan for Bluetooth LE advertisements; the Java
	// side queues what it sees as JSON. StopBLEScan ends it, and TakeBLEEvent
	// pulls one queued event.
	StartBLEScan(json string) (int, error)
	StopBLEScan() error
	TakeBLEEvent() (string, bool)
	// BLEConnect connects to a peripheral and discovers its services; the
	// outcome arrives as events from TakeBLEEvent. BLE{Disconnect,Read,Write,
	// Subscribe} act on that connection. All five need the Bluetooth connect
	// permission on Android 12 and later.
	BLEConnect(json string) error
	BLEDisconnect(json string) error
	BLERead(json string) error
	BLEWrite(json string) error
	BLESubscribe(json string) error
	// BLEBond starts pairing with a device, and BLERequestMTU asks the
	// connected one for a larger packet size. Both report an event when
	// done.
	BLEBond(json string) error
	BLERequestMTU(json string) error
	// RequestBatteryExemption asks for it with the system's own prompt, and
	// returns once the prompt or settings page is up.
	RequestBatteryExemption() error
}

// Position is where the device is, as the system reports it.
type Position struct {
	Latitude  float64
	Longitude float64
	// Accuracy is the radius the system gives, in metres.
	Accuracy float64
	// Time is when the fix was taken; the zero time means unknown.
	Time time.Time
}

// MobileHandler receives system events, including events from an earlier run.
type MobileHandler interface {
	// NotificationReceived returns the foreground presentation bitmask.
	NotificationReceived(NotificationEvent) uint32
	PushRegistered(token string, err error)
}

type NotificationEvent struct {
	ID, Source, Action string
	Data               map[string]string
	Clicked            bool
}

type UnsupportedMobile struct{}

func (UnsupportedMobile) Haptic(string) error              { return ErrUnsupported }
func (UnsupportedMobile) SetBadge(_ int, done func(error)) { done(ErrUnsupported) }
func (UnsupportedMobile) SetStatusBar(string, bool) error  { return ErrUnsupported }
func (UnsupportedMobile) RegisterPush() error              { return ErrUnsupported }
func (UnsupportedMobile) Device() DeviceInfo               { return HostDevice() }
func (UnsupportedMobile) DismissKeyboard()                 {}
func (UnsupportedMobile) ScanCode(_ ScanOptions, done func(string, error)) func() {
	done("", ErrUnsupported)
	return func() {}
}
func (UnsupportedMobile) PrepareNetwork(_ string, _ time.Duration, done func(error)) func() {
	done(nil)
	return func() {}
}
func (UnsupportedMobile) SetBackHandler(func() bool) {}
func (UnsupportedMobile) KeepRunning(string) (func(), error) {
	return nil, ErrUnsupported
}
func (UnsupportedMobile) Location() (Position, error) {
	return Position{}, ErrUnsupported
}
func (UnsupportedMobile) UpdateWidget(string) error { return ErrUnsupported }
func (UnsupportedMobile) BatteryExempt() (bool, error) {
	return true, nil
}
func (UnsupportedMobile) RequestBatteryExemption() error             { return ErrUnsupported }
func (UnsupportedMobile) SetSecureScreen(bool) error                 { return ErrUnsupported }
func (UnsupportedMobile) UpdateTile(string) error                    { return ErrUnsupported }
func (UnsupportedMobile) Speak(string, float64, float64, bool) error { return ErrUnsupported }
func (UnsupportedMobile) StopSpeaking() error                        { return ErrUnsupported }
func (UnsupportedMobile) BLEConnect(string) error                    { return ErrUnsupported }
func (UnsupportedMobile) BLEDisconnect(string) error                 { return ErrUnsupported }
func (UnsupportedMobile) BLERead(string) error                       { return ErrUnsupported }
func (UnsupportedMobile) BLEWrite(string) error                      { return ErrUnsupported }
func (UnsupportedMobile) BLESubscribe(string) error                  { return ErrUnsupported }
func (UnsupportedMobile) BLEBond(string) error                       { return ErrUnsupported }
func (UnsupportedMobile) BLERequestMTU(string) error                 { return ErrUnsupported }
func (UnsupportedMobile) StartBLEScan(string) (int, error)           { return 1, ErrUnsupported }
func (UnsupportedMobile) StopBLEScan() error                         { return ErrUnsupported }
func (UnsupportedMobile) TakeBLEEvent() (string, bool)               { return "", false }

// DeviceInfo identifies the device to people, e.g. to name a paired phone.
type DeviceInfo struct{ Name, System, Version, Model string }

// HostDevice describes a desktop from its host name and operating system.
func HostDevice() DeviceInfo {
	name, _ := os.Hostname()
	system := map[string]string{"darwin": "macOS", "linux": "Linux", "windows": "Windows"}[runtime.GOOS]
	if system == "" {
		system = runtime.GOOS
	}
	return DeviceInfo{Name: name, System: system}
}

// ScanOptions labels the camera scanner. Empty strings use system wording.
type ScanOptions struct{ Prompt, CancelLabel string }

var (
	ErrScanCanceled      = errors.New("mygo: scanning was canceled")
	ErrCameraDenied      = errors.New("mygo: the user does not allow camera access")
	ErrCameraUnavailable = errors.New("mygo: no camera is available")
)

// NetworkError is a failed network preparation. Kind is "offline" without a
// usable connection (or with cellular data disabled for the app), "timeout"
// when the request did not finish in time, and "failed" otherwise.
type NetworkError struct{ Kind string }

func (e *NetworkError) Error() string { return "mygo: network preparation " + e.Kind }
