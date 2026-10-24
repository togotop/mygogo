//go:build android && cgo

package backend

// The phone capabilities Android has: scanning the desktop's QR code, the
// haptic, the keyboard. The rest — badge, status bar, push — are unsupported
// and come from the embedded unsupported backend.
//
// Scanning is the system's camera app, not a preview of ours: ACTION_PICK an
// image with ACTION_IMAGE_CAPTURE, or the photo the person already has. That
// needs no CAMERA permission of this app (the camera app has it), no preview
// surface to manage, and no decoder compiled in — the Java side decodes with
// the platform's own in MygoPlatform.scanImage.

import (
	"encoding/json"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/egoist/mygo/internal/platform"
)

func (b *Backend) Mobile() platform.Mobile { return androidMobile{b: b} }

type androidMobile struct{ b *Backend }

// SetBackHandler hands the handler to the Activity's back gesture, which asks
// it before it leaves the app.
func (m androidMobile) SetBackHandler(fn func() bool) { m.b.SetBackHandler(fn) }

// Haptic is the system's own feedback, and there is no JNI method for it yet:
// reporting it unsupported is honest, and the ui package falls back to nothing.
// Haptic plays the device's own feedback; whether the hardware emitted it is
// the platform's to decide (a device without a vibrator reports unsupported).
func (androidMobile) Haptic(kind string) error {
	b := platformBridge()
	if b == nil {
		return ErrPlatformBridge
	}
	return b.Haptic(kind)
}

// SetBadge reports unsupported: Android has no standard app-icon badge API.
// Calling done is what matters: the caller's await has no timeout, so a
// backend that never answers would hang App.SetNotificationBadge forever.
func (androidMobile) SetBadge(_ int, done func(error)) { done(platform.ErrUnsupported) }

// SetStatusBar applies the bar style and visibility through the Java side.
func (androidMobile) SetStatusBar(style string, hidden bool) error {
	b := platformBridge()
	if b == nil {
		return ErrPlatformBridge
	}
	return b.SetStatusBar(style, hidden)
}

// RegisterPush starts FCM registration; the token arrives through the bridge.
func (androidMobile) RegisterPush() error {
	b := platformBridge()
	if b == nil {
		return ErrPlatformBridge
	}
	return b.RegisterPush()
}

// KeepRunning runs the app in a foreground service, with reason in its
// notification, until the returned function ends it. Android 14 requires the
// service type declared in the manifest (connectedDevice here) and the matching
// permission; the caller is responsible for having a reason the type allows.
func (m androidMobile) KeepRunning(reason string) (func(), error) {
	bridge := platformBridge()
	if bridge == nil {
		return nil, ErrPlatformBridge
	}
	if err := bridge.StartForeground(reason); err != nil {
		return nil, err
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			// The release may run on any goroutine (a timer, a request), and
			// Java is called from the pump: every other backend call does the
			// same, and a JNI call from a goroutine that migrates between OS
			// threads is not something this file promises.
			m.b.Post(func() { _ = bridge.StopForeground() })
		})
	}, nil
}

// UpdateWidget hands the home screen widget's content to the Java side.
func (m androidMobile) UpdateWidget(json string) error {
	bridge := platformBridge()
	if bridge == nil {
		return ErrPlatformBridge
	}
	return bridge.UpdateWidget(json)
}

// SetSecureScreen keeps the window's content out of screenshots.
func (m androidMobile) SetSecureScreen(on bool) error {
	bridge := platformBridge()
	if bridge == nil {
		return ErrPlatformBridge
	}
	return bridge.SetSecureScreen(on)
}

// StartBLEScan starts a Bluetooth LE scan; the Java side queues what it sees.
func (m androidMobile) StartBLEScan(json string) (int, error) {
	bridge := platformBridge()
	if bridge == nil {
		return 3, ErrPlatformBridge
	}
	switch rc := bridge.StartBLEScan(json); rc {
	case 0:
		return 0, nil
	case 1:
		return 1, platform.ErrPermissionDenied
	case 2:
		return 2, platform.ErrUnsupported
	default:
		return 3, ErrBluetoothScan
	}
}

// StopBLEScan ends the scan.
func (m androidMobile) StopBLEScan() error {
	if bridge := platformBridge(); bridge != nil {
		bridge.StopBLEScan()
	}
	return nil
}

// TakeBLEEvent pulls one queued scan event, as JSON.
func (m androidMobile) TakeBLEEvent() (string, bool) {
	if bridge := platformBridge(); bridge != nil {
		return bridge.TakeBLEEvent()
	}
	return "", false
}

// bleGATT maps the Java status code of a GATT call to an error.
func bleGATT(rc int) error {
	switch rc {
	case 0:
		return nil
	case 1:
		return platform.ErrPermissionDenied
	case 2:
		return platform.ErrUnsupported
	}
	return ErrBluetoothGATT
}

// BLEConnect starts a connection to the peripheral at the address in json.
func (m androidMobile) BLEConnect(json string) error {
	bridge := platformBridge()
	if bridge == nil {
		return ErrPlatformBridge
	}
	return bleGATT(bridge.BLEConnect(json))
}

// BLEDisconnect drops the connection.
func (m androidMobile) BLEDisconnect(json string) error {
	bridge := platformBridge()
	if bridge == nil {
		return ErrPlatformBridge
	}
	return bleGATT(bridge.BLEDisconnect(json))
}

// BLERead asks for a characteristic value; it arrives as an event.
func (m androidMobile) BLERead(json string) error {
	bridge := platformBridge()
	if bridge == nil {
		return ErrPlatformBridge
	}
	return bleGATT(bridge.BLERead(json))
}

// BLEWrite writes a characteristic value.
func (m androidMobile) BLEWrite(json string) error {
	bridge := platformBridge()
	if bridge == nil {
		return ErrPlatformBridge
	}
	return bleGATT(bridge.BLEWrite(json))
}

// BLESubscribe turns notifications for a characteristic on or off.
func (m androidMobile) BLESubscribe(json string) error {
	bridge := platformBridge()
	if bridge == nil {
		return ErrPlatformBridge
	}
	return bleGATT(bridge.BLESubscribe(json))
}

// BLEBond starts pairing with the device at the address in json.
func (m androidMobile) BLEBond(json string) error {
	bridge := platformBridge()
	if bridge == nil {
		return ErrPlatformBridge
	}
	return bleGATT(bridge.BLEBond(json))
}

// BLERequestMTU asks the connected device for a larger packet size.
func (m androidMobile) BLERequestMTU(json string) error {
	bridge := platformBridge()
	if bridge == nil {
		return ErrPlatformBridge
	}
	return bleGATT(bridge.BLERequestMTU(json))
}

// androidMobile is what the Go side asserts for the media controls and the
// NFC reader; the assertions keep the compiler checking the method names,
// which a type assertion would otherwise only reveal on a device.
var (
	_ platform.AudioControl = androidMobile{}
	_ platform.NFCReader    = androidMobile{}
)

// HostEvent hands one JSON message back to the host.
func (m androidMobile) HostEvent(json string) int {
	if bridge := platformBridge(); bridge != nil {
		return bridge.HostEvent(json)
	}
	return -1
}

// NFCAvailable reports whether the device has an NFC adapter.
func (m androidMobile) NFCAvailable() bool {
	if bridge := platformBridge(); bridge != nil {
		return bridge.NFCAvailable()
	}
	return false
}

// NFCScanStart enables the foreground reader.
func (m androidMobile) NFCScanStart() int {
	if bridge := platformBridge(); bridge != nil {
		return bridge.NFCScanStart()
	}
	return 3
}

// NFCScanStop disables it.
func (m androidMobile) NFCScanStop() int {
	if bridge := platformBridge(); bridge != nil {
		return bridge.NFCScanStop()
	}
	return 3
}

// TakeNFCEvent pulls one queued tag, as JSON.
func (m androidMobile) TakeNFCEvent() (string, bool) {
	if bridge := platformBridge(); bridge != nil {
		return bridge.TakeNFCEvent()
	}
	return "", false
}

// NFCWriteText writes a text record to the tag in the field.
func (m androidMobile) NFCWriteText(json string) int {
	if bridge := platformBridge(); bridge != nil {
		return bridge.NFCWriteText(json)
	}
	return 3
}

// Transceive sends one raw command to the tag in the field.
func (m androidMobile) Transceive(json string) (string, bool) {
	if bridge := platformBridge(); bridge != nil {
		return bridge.NFCTransceive(json)
	}
	return "", false
}

// HCEStart turns on card emulation.
func (m androidMobile) HCEStart(json string) int {
	if bridge := platformBridge(); bridge != nil {
		return bridge.HCEStart(json)
	}
	return 3
}

// HCEStop turns it off.
func (m androidMobile) HCEStop() int {
	if bridge := platformBridge(); bridge != nil {
		return bridge.HCEStop()
	}
	return 3
}

// TakeHCERequest pulls one waiting APDU, as JSON.
func (m androidMobile) TakeHCERequest() (string, bool) {
	if bridge := platformBridge(); bridge != nil {
		return bridge.TakeHCERequest()
	}
	return "", false
}

// HCERespond hands the answer back to the waiting service.
func (m androidMobile) HCERespond(json string) int {
	if bridge := platformBridge(); bridge != nil {
		return bridge.HCERespond(json)
	}
	return 3
}

// audioBridge is the Java half of the media controls, or nil when the
// platform never attached.
func audioBridge() PlatformBridge { return platformBridge() }

// AudioVolume reports the media volume and ringer mode.
func (m androidMobile) Volume() (platform.AudioVolumeInfo, error) {
	bridge := audioBridge()
	if bridge == nil {
		return platform.AudioVolumeInfo{}, ErrPlatformBridge
	}
	raw, ok := bridge.AudioVolume()
	if !ok {
		return platform.AudioVolumeInfo{}, ErrPlatformBridge
	}
	var info platform.AudioVolumeInfo
	if err := json.Unmarshal([]byte(raw), &info); err != nil {
		return platform.AudioVolumeInfo{}, err
	}
	return info, nil
}

// AudioSetVolume sets, mutes or steps the media volume.
func (m androidMobile) SetVolume(json string) error {
	bridge := audioBridge()
	if bridge == nil {
		return ErrPlatformBridge
	}
	return audioStatus(bridge.AudioSetVolume(json))
}

// AudioMediaKey sends one transport key.
func (m androidMobile) MediaKey(json string) error {
	bridge := audioBridge()
	if bridge == nil {
		return ErrPlatformBridge
	}
	return audioStatus(bridge.AudioMediaKey(json))
}

// audioStatus maps the Java status code of a media control call.
func audioStatus(rc int) error {
	switch rc {
	case 0:
		return nil
	case 1:
		return platform.ErrPermissionDenied
	}
	return ErrAudioControl
}

// AudioRingerMode sets the ringer mode; ErrDndAccess means silent is not
// permitted yet.
func (m androidMobile) RingerMode(json string) error {
	bridge := audioBridge()
	if bridge == nil {
		return ErrPlatformBridge
	}
	rc := bridge.AudioRingerMode(json)
	switch rc {
	case 0:
		return nil
	case 2:
		return platform.ErrDndAccess
	}
	return audioStatus(rc)
}

// SilenceAllowed reports whether silent is permitted.
func (m androidMobile) SilenceAllowed() bool {
	if bridge := audioBridge(); bridge != nil {
		return bridge.SilenceAllowed()
	}
	return false
}

// OpenSilenceSettings shows the system page that grants it.
func (m androidMobile) OpenSilenceSettings() error {
	bridge := audioBridge()
	if bridge == nil {
		return ErrPlatformBridge
	}
	if bridge.OpenSilenceSettings() != 0 {
		return ErrSilenceSettings
	}
	return nil
}

// Speak hands text to the system's speech synthesis.
func (m androidMobile) Speak(text string, rate, pitch float64, flush bool) error {
	bridge := platformBridge()
	if bridge == nil {
		return ErrPlatformBridge
	}
	payload := struct {
		Text  string  `json:"text"`
		Rate  float64 `json:"rate"`
		Pitch float64 `json:"pitch"`
		Flush bool    `json:"flush"`
	}{text, rate, pitch, flush}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	switch rc := bridge.Speak(string(encoded)); rc {
	case 0:
		return nil
	case 1:
		return platform.ErrSpeechStarting
	case 2:
		return platform.ErrUnsupported
	default:
		return ErrSpeechSynthesis
	}
}

// StopSpeaking stops whatever synthesis is playing.
func (m androidMobile) StopSpeaking() error {
	if bridge := platformBridge(); bridge != nil {
		bridge.StopSpeaking()
	}
	return nil
}

// UpdateTile hands the quick settings tile's content to the Java side.
func (m androidMobile) UpdateTile(json string) error {
	bridge := platformBridge()
	if bridge == nil {
		return ErrPlatformBridge
	}
	return bridge.UpdateTile(json)
}

// BatteryExempt reports whether the system lets the app run unrestricted.
func (m androidMobile) BatteryExempt() (bool, error) {
	bridge := platformBridge()
	if bridge == nil {
		return false, ErrPlatformBridge
	}
	return bridge.BatteryExempt()
}

// RequestBatteryExemption shows the system's exemption prompt.
func (m androidMobile) RequestBatteryExemption() error {
	bridge := platformBridge()
	if bridge == nil {
		return ErrPlatformBridge
	}
	return bridge.RequestBatteryExemption()
}

// Location is the last fix the system recorded, which may be stale: Time says
// when it was taken. No fix and no permission both report ErrUnsupported; the
// caller can ask Permissions.Query("geolocation") to tell them apart.
func (m androidMobile) Location() (platform.Position, error) {
	bridge := platformBridge()
	if bridge == nil {
		return platform.Position{}, ErrPlatformBridge
	}
	lat, lon, accuracy, at, ok := bridge.Location()
	if !ok {
		return platform.Position{}, platform.ErrUnsupported
	}
	pos := platform.Position{Latitude: lat, Longitude: lon, Accuracy: accuracy}
	if at > 0 {
		pos.Time = time.UnixMilli(at)
	}
	return pos, nil
}

// Device names this phone to desktops: the name the owner gave it, or the
// model, with the Android version. Without the bridge it falls back to the
// host's answer, which is at least right about the OS.
func (androidMobile) Device() platform.DeviceInfo {
	b := platformBridge()
	if b == nil {
		return platform.HostDevice()
	}
	info, err := b.DeviceInfo()
	if err != nil || info.Name == "" {
		return platform.HostDevice()
	}
	return info
}

// DismissKeyboard ends editing, which is what lowers the keyboard: the Ime
// tells the host it is inactive, and the host hides the soft input.
func (androidMobile) DismissKeyboard() {
	TheIme().SetTextInput(platform.TextInputState{})
}
func (androidMobile) PrepareNetwork(_ string, _ time.Duration, done func(error)) func() {
	done(nil)
	return func() {}
}

// ScanCode opens the system's image picker and decodes what comes back.
//
// The prompt and the cancel label are the Java side's to show, in its own
// dialog; they are passed and not used by the picker itself, which has its own
// chrome — passing them keeps the platform call one shape on both phones.
func (m androidMobile) ScanCode(o platform.ScanOptions, done func(string, error)) func() {
	if platformBridge() == nil {
		done("", platform.ErrUnsupported)
		return func() {}
	}
	token := nextToken()
	scanTokens.Store(token, done)
	log.Printf("mygo/android: scan: ScanCode token=%d bridge=%v", token, platformBridge() != nil)
	if err := platformBridge().ScanCode(token, o.Prompt); err != nil {
		scanTokens.Delete(token)
		done("", err)
	}
	return func() {
		if _, ok := scanTokens.LoadAndDelete(token); ok {
			_ = platformBridge().ScanCancel(token)
		}
	}
}

// errScanFailed is a decode that ran and found nothing: a picture with no QR
// code in it is the common case, and not something the person needs to read a
// stack for.
var errScanFailed = errors.New("mygo: no QR code was found in the picture")

var (
	scanMu     sync.Mutex
	scanToken  uint64
	scanTokens sync.Map // token → func(string, error)
)

func nextToken() uint64 {
	scanMu.Lock()
	defer scanMu.Unlock()
	scanToken++
	return scanToken
}

// ScanResult is what the Java side reports, on the pump: the decoded text, or
// the reason there is none. code is platform's own vocabulary over JNI — 0 for
// a value, 5 canceled, 6 denied, 7 no camera, 1 unsupported, 2 anything else.
func ScanResult(token uint64, text string, code int32) {
	v, ok := scanTokens.LoadAndDelete(token)
	if !ok {
		// Canceled by the caller while the picker was up: the answer is dropped,
		// which is what cancel means.
		return
	}
	done := v.(func(string, error))
	log.Printf("mygo/android: ScanResult code=%d len=%d head=%q", code, len(text), truncate(text, 40))
	switch code {
	case 0:
		if text == "" {
			done("", platform.ErrScanCanceled)
			return
		}
		done(text, nil)
	case 5:
		done("", platform.ErrScanCanceled)
	case 6:
		done("", platform.ErrCameraDenied)
	case 7:
		done("", platform.ErrCameraUnavailable)
	case 1:
		done("", platform.ErrUnsupported)
	default:
		done("", errScanFailed)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
