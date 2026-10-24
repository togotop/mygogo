package backend

import (
	"sync"

	"github.com/egoist/mygo/internal/platform"
)

// The app handler Init stored, for results that arrive without a caller of
// their own: a push token comes from Firebase, not from a Go call.
var (
	handlerMu  sync.RWMutex
	handlerRef platform.AppHandler
)

func rememberHandler(h platform.AppHandler) {
	handlerMu.Lock()
	handlerRef = h
	handlerMu.Unlock()
}

// pushHandler is implemented by app handlers that accept data-only pushes.
// It is a separate interface so every other platform keeps compiling without
// the message: iOS pushes arrive through the system and never take this path.
type pushHandler interface {
	PushMessage(payload string)
}

// PushMessage delivers a data-only push to the app handler. A process raised
// only to receive it has no app running, which is why the host shows a
// notification itself; here there is nothing to update, so it is dropped.
func PushMessage(payload string) {
	handlerMu.RLock()
	h := handlerRef
	handlerMu.RUnlock()
	if h == nil {
		return
	}
	if ph, ok := h.(pushHandler); ok {
		ph.PushMessage(payload)
	}
}

// PushRegistered delivers a token, or the reason there is none, to the app's
// MobileHandler, which is where OnPushToken and OnPushRegistrationError listen.
func PushRegistered(token string, err error) {
	handlerMu.RLock()
	h := handlerRef
	handlerMu.RUnlock()
	if h == nil {
		return
	}
	if mh, ok := h.(platform.MobileHandler); ok {
		mh.PushRegistered(token, err)
	}
}
