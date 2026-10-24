//go:build android && cgo

package backend

// Android's authorization model over platform.Permissions.
//
// Query never prompts: it reads the grant the system already recorded. Request
// can wait for a person, so it returns as soon as the prompt is up and the
// answer arrives later through PermissionResult — the same token the caller
// gave, the way ScanCode answers. The prompt may be answered while the app is
// in the background, so internal/android polls the Java side for the answer
// (see collectPermissionResults) instead of waiting on a callback.
//
// The microphone and "speech" are one grant on Android: MyGo's Speech.Authorize
// asks for both, and a platform that answered differently for the two would make
// the second call contradict the first. See permissionKind.

import (
	"errors"
	"log"
	"sync"
	"time"

	"github.com/egoist/mygo/internal/platform"
)

// errPermissionTimeout is the answer when a prompt is never answered, which
// happens when the Activity goes away with the dialog up. The top-level API
// waits without a timeout, so this is what keeps a caller from waiting forever.
var errPermissionTimeout = errors.New("mygo: the permission prompt was never answered")

// permissionPromptTimeout bounds a prompt nobody answers. It is generous: the
// person may be reading it, and the collector's poll notices as soon as they
// answer.
const permissionPromptTimeout = 10 * time.Minute

var permissionTokens sync.Map // token → func(string, error)

func nextPermissionToken() uint64 {
	return nextToken()
}

// Permissions returns Android's permissions.
func (b *Backend) Permissions() platform.Permissions { return androidPermissions{b: b} }

type androidPermissions struct{ b *Backend }

// Query reads the grant without prompting.
func (androidPermissions) Query(name string, done func(string, error)) {
	kind := permissionKind(name)
	bridge := platformBridge()
	if kind < 0 {
		done("", platform.ErrUnsupported)
		return
	}
	if bridge == nil {
		done("", ErrPlatformBridge)
		return
	}
	done(permissionStatusName(bridge.PermissionStatus(kind)), nil)
}

// Request asks the system, and answers through PermissionResult.
func (androidPermissions) Request(name string, done func(string, error)) {
	kind := permissionKind(name)
	bridge := platformBridge()
	if kind < 0 {
		done("", platform.ErrUnsupported)
		return
	}
	if bridge == nil {
		done("", ErrPlatformBridge)
		return
	}
	token := nextPermissionToken()
	permissionTokens.Store(token, done)
	time.AfterFunc(permissionPromptTimeout, func() { permissionTimedOut(token) })
	if err := bridge.RequestPermission(kind, token); err != nil {
		permissionTokens.Delete(token)
		done("", err)
	}
}

// OpenSettings opens the app's page in the system settings.
func (androidPermissions) OpenSettings(done func(error)) {
	bridge := platformBridge()
	if bridge == nil {
		done(ErrPlatformBridge)
		return
	}
	done(bridge.OpenAppSettings())
}

// permissionTimedOut answers a request whose prompt outlived its deadline,
// unless an answer arrived first.
func permissionTimedOut(token uint64) {
	v, ok := permissionTokens.LoadAndDelete(token)
	if !ok {
		return
	}
	log.Printf("mygo/android: permissions: token=%d was never answered", token)
	v.(func(string, error))("", errPermissionTimeout)
}

// PermissionResult answers one request with the status the Java side reported:
// 0 granted, 1 denied, 2 not determined. internal/android calls it from the
// goroutine that collected the answer.
func PermissionResult(token uint64, status int) {
	v, ok := permissionTokens.LoadAndDelete(token)
	if !ok {
		// Already answered, or a late result after the timeout: the first
		// answer is the one that counts.
		return
	}
	v.(func(string, error))(permissionStatusName(status), nil)
}
