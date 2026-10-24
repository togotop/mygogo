//go:build android && cgo

package mygo

import androidbackend "github.com/egoist/mygo/internal/android/backend"

// platformDirs returns the directories the host reported, which are the only
// ones an Android app may write.
func platformDirs() (data, cache string, ok bool) {
	data, cache = androidbackend.DataDir(), androidbackend.CacheDir()
	return data, cache, data != ""
}
