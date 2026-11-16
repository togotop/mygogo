package mygo

import (
	"time"

	"github.com/egoist/mygo/async"
)

// NewTask delivers work and progress on the UI thread, and pauses what it runs
// when the application stops being active: a request it started does not rerun
// by itself when the app comes back, Resume takes new work. Call Close to
// detach it, and Cancel when leaving the page that made it.
func NewTask[T any](notify func(async.State[T])) *async.Task[T] {
	needsApp("NewTask")
	t := async.New(func(fn func()) { postMain(fn) }, notify)
	off := App.OnLifecycleChanged(func(state LifecycleState) {
		if state == LifecycleActive {
			t.Resume()
		} else {
			t.Pause()
		}
	})
	offQuit := App.OnQuit(t.Close)
	t.SetCleanup(func() { off(); offQuit() })
	onMain(func() {
		if App.Lifecycle() != LifecycleActive {
			t.Pause()
		}
	})
	return t
}

// NewPoller polls work every interval, which also wants SetVisible(true)
// before it starts and keeps its timers stopped while the view is hidden or
// the application is not active.
func NewPoller[T any](interval time.Duration, work async.Work[T], notify func(async.State[T])) *async.Poller[T] {
	needsApp("NewPoller")
	t := async.New(func(fn func()) { postMain(fn) }, notify)
	p := async.NewPoller(t, interval, work)
	off := App.OnLifecycleChanged(func(s LifecycleState) { p.SetForeground(s == LifecycleActive) })
	offQuit := App.OnQuit(p.Close)
	t.SetCleanup(func() { off(); offQuit() })
	onMain(func() { p.SetForeground(App.Lifecycle() == LifecycleActive) })
	return p
}
