package backend

import (
	"errors"
	"sync"
	"sync/atomic"

	"github.com/egoist/mygo/internal/platform"
)

// The mobile pickers: the photo picker and the document export picker. Both
// are Java activities. Go starts one through the bridge, and the answer comes
// back the way ScanCode's does — the Java side parks it in a pending slot and
// a Go goroutine polls for it.

// ErrPickerUnavailable is the Java side saying it could not start the picker
// activity at all (no activity to handle the intent, or the system refused).
var ErrPickerUnavailable = errors.New("mygo: Android could not open the picker")

var (
	photoTokens  sync.Map // token → func([]string, error)
	exportTokens sync.Map // token → func(bool, error)
	mediaToken   atomic.Uint64
)

func nextMediaToken() uint64 { return mediaToken.Add(1) }

// androidDialogs is platform.Dialogs with the two mobile pickers overridden.
// The rest (open, save, message box) stays the embedded unsupported answer.
type androidDialogs struct{ platform.Dialogs }

// ShowPhotoDialog opens the system photo picker. Paths refer to private cache
// copies, which the caller may read after dismissal and should remove.
func (androidDialogs) ShowPhotoDialog(_ platform.Window, o *platform.PhotoDialogOptions, done func([]string, error)) {
	b := platformBridge()
	if b == nil {
		done(nil, ErrPlatformBridge)
		return
	}
	token := nextMediaToken()
	photoTokens.Store(token, done)
	multiple := o != nil && o.Multiple
	if err := b.PickPhotos(token, multiple); err != nil {
		photoTokens.Delete(token)
		done(nil, err)
	}
}

// ShowExportDialog copies one existing file to a destination the person picks.
//
// One file first: SAF creates one document per pick, and exporting several
// silently one by one would be a different feature. A caller that passes more
// learns the rest is unsupported rather than losing them.
func (androidDialogs) ShowExportDialog(_ platform.Window, o *platform.ExportDialogOptions, done func(bool, error)) {
	b := platformBridge()
	if b == nil {
		done(false, ErrPlatformBridge)
		return
	}
	if o == nil || len(o.Files) == 0 {
		done(false, errors.New("mygo: export requires at least one file"))
		return
	}
	if len(o.Files) > 1 {
		done(false, platform.ErrUnsupported)
		return
	}
	token := nextMediaToken()
	exportTokens.Store(token, done)
	if err := b.ExportFile(token, o.Files[0], o.Title); err != nil {
		exportTokens.Delete(token)
		done(false, err)
	}
}

// PhotoResult delivers a picked set to its caller. An empty set is the person
// cancelling, which the contract reports as nil, nil.
func PhotoResult(token uint64, paths []string) {
	v, ok := photoTokens.LoadAndDelete(token)
	if !ok {
		return
	}
	done := v.(func([]string, error))
	if len(paths) == 0 {
		done(nil, nil)
		return
	}
	done(paths, nil)
}

// ExportResult delivers the export's outcome: completed false is a cancel.
func ExportResult(token uint64, completed bool) {
	v, ok := exportTokens.LoadAndDelete(token)
	if !ok {
		return
	}
	v.(func(bool, error))(completed, nil)
}
