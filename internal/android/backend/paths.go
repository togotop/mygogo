//go:build android && cgo

package backend

import "sync"

// An Android app has no home directory, and os.UserHomeDir answers /sdcard,
// which it cannot write: the only directories it may use are the ones the
// host hands it. They arrive once, before the app starts.
var dirs struct {
	mu    sync.RWMutex
	data  string
	cache string
}

// SetDataDirs records the app's private directories, as the host passed them.
func SetDataDirs(data, cache string) {
	dirs.mu.Lock()
	dirs.data, dirs.cache = data, cache
	dirs.mu.Unlock()
}

// DataDir returns the app's private data directory, or "" before the host
// reported it.
func DataDir() string {
	dirs.mu.RLock()
	defer dirs.mu.RUnlock()
	return dirs.data
}

// CacheDir returns the app's private cache directory, or "" before the host
// reported it.
func CacheDir() string {
	dirs.mu.RLock()
	defer dirs.mu.RUnlock()
	return dirs.cache
}
