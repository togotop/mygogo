// Package netpath tells how the device reaches the network: whether it is
// online, over what, and whether that costs the user data. An app that syncs
// when it can, or holds back on a metered link, asks it.
//
//	path, err := netpath.Current(ctx)
//	if path.Online && !path.Expensive { sync() }
//
//	stop, err := netpath.Watch(ctx, func(p netpath.Path) { update(p) })
//	defer stop()
//
// Elsewhere its functions report errors.ErrUnsupported.
package netpath

import (
	"context"
	"encoding/json"

	"github.com/egoist/mygo"
)

const pluginID = "netpath"

// Path is how the device reaches the network.
type Path struct {
	// Known says whether the system could tell: false where it has no such
	// API, and the other fields then say nothing.
	Known bool `json:"known"`
	// Online is true when the device reaches the internet, not merely a
	// network: a Wi-Fi that asks for a sign-in is not.
	Online bool `json:"online"`
	// Interface is how: "wifi", "cellular", "ethernet", "vpn", "other" or
	// "none" while offline.
	Interface string `json:"interface"`
	// Expensive marks a link the user pays for, as a metered cellular one,
	// and Constrained a link the system asks apps to go easy on.
	Expensive   bool `json:"expensive"`
	Constrained bool `json:"constrained"`
}

// Current tells how the device reaches the network now.
func Current(ctx context.Context) (Path, error) {
	var p Path
	if err := mygo.Native(pluginID).Call(ctx, "current", nil, &p); err != nil {
		return Path{}, err
	}
	return p, nil
}

// Watcher hears the path changing until Stop.
type Watcher struct{ stop func() }

// Stop ends the watching.
func (w *Watcher) Stop() {
	if w != nil && w.stop != nil {
		w.stop()
		w.stop = nil
	}
}

// Watch reports each change of the path to fn, until Stop. The callback runs
// on the goroutine that called Watch.
func Watch(ctx context.Context, fn func(Path)) (*Watcher, error) {
	events := make(chan Path, 8)
	off := mygo.Native(pluginID).On("event", func(data []byte) {
		var p Path
		if json.Unmarshal(data, &p) != nil {
			return
		}
		select {
		case events <- p:
		default:
			// A change that does not fit is one the next one supersedes.
		}
	})
	if err := mygo.Native(pluginID).Call(ctx, "watch", nil, nil); err != nil {
		off()
		return nil, err
	}
	done := make(chan struct{})
	go func() {
		for {
			select {
			case p := <-events:
				fn(p)
			case <-done:
				return
			}
		}
	}()
	return &Watcher{stop: func() {
		close(done)
		off()
		_ = mygo.Native(pluginID).Call(context.Background(), "stopWatch", nil, nil)
	}}, nil
}
