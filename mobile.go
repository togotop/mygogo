package mygo

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/egoist/mygo/internal/platform"
	"maps"
	"strings"
)

// HapticFeedback is a short system feedback pattern.
type HapticFeedback string

const (
	HapticSelection HapticFeedback = "selection"
	HapticLight     HapticFeedback = "light"
	HapticMedium    HapticFeedback = "medium"
	HapticHeavy     HapticFeedback = "heavy"
	HapticSoft      HapticFeedback = "soft"
	HapticRigid     HapticFeedback = "rigid"
	HapticSuccess   HapticFeedback = "success"
	HapticWarning   HapticFeedback = "warning"
	HapticError     HapticFeedback = "error"
)

// Haptics provides device feedback. A successful call does not imply the
// hardware emitted a vibration (simulators and system settings may suppress it).
var Haptics hapticsModule

type hapticsModule struct{}

func (hapticsModule) Play(kind HapticFeedback) error {
	switch kind {
	case HapticSelection, HapticLight, HapticMedium, HapticHeavy, HapticSoft, HapticRigid, HapticSuccess, HapticWarning, HapticError:
	default:
		return fmt.Errorf("mygo: unknown haptic %q", kind)
	}
	needsApp("Haptics.Play")
	return onMainValue(func() error { return backend().Mobile().Haptic(string(kind)) })
}

// OnBack installs fn, which the system's back gesture calls: it reports
// whether the app went back a page, and the system leaves the app when it
// did not.
//
// fn runs on the thread the gesture arrives on, which is not the one the
// interface is built on, so it must return quickly and only read state. What
// it starts goes through RunOnMain:
//
//	mygo.App.OnBack(func() bool {
//		if !app.canGoBack() {
//			return false
//		}
//		mygo.RunOnMain(app.goBack)
//		return true
//	})
//
// A platform without the gesture, such as iOS, never calls it.
func (a *Application) OnBack(fn func() bool) {
	needsApp("App.OnBack")
	backend().Mobile().SetBackHandler(fn)
}

// StatusBarStyle controls system text; auto follows the window background.
type StatusBarStyle string

const (
	StatusBarAuto  StatusBarStyle = ""
	StatusBarLight StatusBarStyle = "light"
	StatusBarDark  StatusBarStyle = "dark"
)

// SetStatusBar sets iOS status bar text and visibility.
func (a *Application) SetStatusBar(style StatusBarStyle, hidden bool) error {
	if style != StatusBarAuto && style != StatusBarLight && style != StatusBarDark {
		return fmt.Errorf("mygo: unknown status bar style %q", style)
	}
	needsApp("App.SetStatusBar")
	return onMainValue(func() error { return backend().Mobile().SetStatusBar(string(style), hidden) })
}

// SetNotificationBadge sets the iOS app icon badge (zero clears it). Requires badge
// authorization; failure is returned without altering the shared Dock API.
func (a *Application) SetNotificationBadge(count int) error {
	if count < 0 {
		return fmt.Errorf("mygo: badge count cannot be negative")
	}
	needsApp("App.SetNotificationBadge")
	ch := make(chan error, 1)
	onMain(func() { backend().Mobile().SetBadge(count, func(err error) { deliver(ch, err) }) })
	return await(ch)
}

// NotificationEvent contains custom string data retained by iOS. Clicked is
// false for foreground delivery and true for a system notification response.
type NotificationEvent struct {
	ID      string
	Data    map[string]string
	Clicked bool
	// Source identifies local scheduling or remote push delivery.
	Source NotificationSource
	// Action is a custom system action identifier; empty means ordinary delivery
	// or opening the notification. Clicked remains true for an ordinary open.
	Action string
}

type NotificationSource string

const (
	NotificationLocal  NotificationSource = "local"
	NotificationRemote NotificationSource = "remote"
)

// NotificationPresentation controls iOS foreground presentation. Zero suppresses
// system UI while still delivering OnNotification. Background presentation is
// controlled by the push payload and the user's system settings.
type NotificationPresentation uint32

const (
	PresentNotificationBanner NotificationPresentation = 1 << iota
	PresentNotificationList
	PresentNotificationSound
	PresentNotificationBadge
	PresentNotificationDefault = PresentNotificationBanner | PresentNotificationList | PresentNotificationSound | PresentNotificationBadge
)

var mobileEvents struct {
	notifications listeners[func(NotificationEvent)]
	tokens        listeners[func(string)]
	errors        listeners[func(error)]
	pushData      listeners[func(map[string]string)]
	widgetActions listeners[func(string)]
	tileClicks    listeners[func()]
	presentation  func(NotificationEvent) NotificationPresentation // main thread
}

// SetNotificationPresentationHandler chooses iOS foreground presentation per
// notification. Register before Run; nil restores the default banner, list,
// sound and badge. The callback runs synchronously on the UI thread before
// OnNotification and must return promptly. It does not request authorization.
// Other platforms retain their native presentation behavior.
func (a *Application) SetNotificationPresentationHandler(fn func(NotificationEvent) NotificationPresentation) {
	onMain(func() { mobileEvents.presentation = fn })
}

// OnNotification receives foreground delivery and notification clicks, also
// after cold launch. Register before App.Run; callbacks run on the UI thread.
func (a *Application) OnNotification(fn func(NotificationEvent)) func() {
	return mobileEvents.notifications.add(fn, false)
}

// RegisterPushNotifications starts APNs registration. Configure aps-environment
// entitlements and a matching provisioning profile; it does not request alert
// permission. Tokens and errors arrive through the listeners below.
func (a *Application) RegisterPushNotifications() error {
	needsApp("App.RegisterPushNotifications")
	return onMainValue(func() error { return backend().Mobile().RegisterPush() })
}
func (a *Application) OnPushToken(fn func(string)) func() { return mobileEvents.tokens.add(fn, false) }

// KeepRunning asks the system to keep the app working in the background,
// showing reason in a persistent notification while it does. The returned
// function ends it, and is safe to call more than once; call it when the work
// is done. Android runs a foreground service, which Android 14 only allows for
// the types the manifest declares. Platforms that cannot grant this return
// platform.ErrUnsupported.
func (a *Application) KeepRunning(reason string) (release func(), err error) {
	needsApp("App.KeepRunning")
	if strings.TrimSpace(reason) == "" {
		return nil, errors.New("mygo: KeepRunning needs a reason")
	}
	onMain(func() { release, err = backend().Mobile().KeepRunning(reason) })
	if err != nil {
		return nil, err
	}
	// The backend already makes the release idempotent; wrapping it here
	// again would only add a second Once to reason about.
	return release, nil
}

// SetSecureScreen keeps the app's content out of screenshots, screen
// recordings and the recents thumbnail, for windows that show secrets such as
// pairing codes or a terminal's output. Platforms without the switch return
// platform.ErrUnsupported.
func (a *Application) SetSecureScreen(on bool) error {
	needsApp("App.SetSecureScreen")
	var err error
	onMain(func() { err = backend().Mobile().SetSecureScreen(on) })
	return err
}

// OnPushMessage receives a data-only remote push, as sent by FCM on Android.
// A message that carries a title or body becomes a notification instead, so
// that it still arrives when the app is not running. Other platforms do not
// deliver this event: their pushes reach the app through the system.
func (a *Application) OnPushMessage(fn func(map[string]string)) func() {
	return mobileEvents.pushData.add(fn, false)
}
func (a *Application) OnPushRegistrationError(fn func(error)) func() {
	return mobileEvents.errors.add(fn, false)
}
func (appHandler) NotificationReceived(input platform.NotificationEvent) uint32 {
	event := NotificationEvent{ID: input.ID, Data: maps.Clone(input.Data), Clicked: input.Clicked, Source: NotificationSource(input.Source), Action: input.Action}
	presentation := PresentNotificationDefault
	if !event.Clicked && mobileEvents.presentation != nil {
		presentation = mobileEvents.presentation(event) & PresentNotificationDefault
	}
	if event.Clicked {
		notificationClicked(event.ID)
	}
	event.Data = maps.Clone(input.Data)
	fire1(&mobileEvents.notifications, event)
	return uint32(presentation)
}
func (appHandler) PushRegistered(token string, err error) {
	if err != nil {
		fire1(&mobileEvents.errors, err)
	} else {
		fire1(&mobileEvents.tokens, token)
	}
}

// TileClick delivers a tapped quick settings tile.
func (appHandler) TileClick() {
	fire(&mobileEvents.tileClicks)
}

// WidgetAction delivers a tapped row of the home screen widget: the app hears
// the row's ID.
func (appHandler) WidgetAction(id string) {
	fire1(&mobileEvents.widgetActions, id)
}

// NotificationAction delivers a notification button press. The app hears it as
// an OnNotification event with Action set, and the ordinary click listeners
// fire as they do for a tap: a press is an interaction with the notification.
func (appHandler) NotificationAction(id, action string) {
	event := NotificationEvent{ID: id, Clicked: true, Action: action}
	notifications.Lock()
	if n := notifications.byID[id]; n != nil {
		event.Data = maps.Clone(n.opts.Data)
	}
	notifications.Unlock()
	notificationClicked(id)
	fire1(&mobileEvents.notifications, event)
}

// PushMessage parses the data-only payload the host forwarded. Malformed JSON
// is dropped rather than surfaced: it carries nothing the app can act on.
func (appHandler) PushMessage(payload string) {
	var data map[string]string
	if err := json.Unmarshal([]byte(payload), &data); err != nil {
		return
	}
	fire1(&mobileEvents.pushData, data)
}
