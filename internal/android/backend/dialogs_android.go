//go:build android && cgo

package backend

import "github.com/egoist/mygo/internal/platform"

// Dialogs returns the mobile pickers over the embedded unsupported dialogs.
func (b *Backend) Dialogs() platform.Dialogs { return androidDialogs{b.Backend.Dialogs()} }
