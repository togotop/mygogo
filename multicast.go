package mygo

import (
	"sync"

	"github.com/egoist/mygo/internal/platform"
)

// multicastUsers counts the browses, searches and advertisements holding the
// Wi-Fi multicast lock: the first asks the platform for it, the last gives it
// back. A phone filters what the local network announces unless the app holds
// the lock, and the lock is the platform's, where its host can take one;
// platforms without it are not asked.
var multicastUsers struct {
	sync.Mutex
	n int
}

func acquireMulticast() {
	multicastUsers.Lock()
	defer multicastUsers.Unlock()
	if multicastUsers.n == 0 {
		if l, ok := backend().(platform.MulticastLocker); ok {
			l.MulticastLock(true)
		}
	}
	multicastUsers.n++
}

func releaseMulticast() {
	multicastUsers.Lock()
	defer multicastUsers.Unlock()
	if multicastUsers.n == 0 {
		return
	}
	multicastUsers.n--
	if multicastUsers.n == 0 {
		if l, ok := backend().(platform.MulticastLocker); ok {
			l.MulticastLock(false)
		}
	}
}
