package mygo

// LifecycleState is the application's activity, independent of readiness: the
// window the user works in is Active, one behind another Inactive, and a
// phone's app in the background Inactive too, as the systems report the two
// the same way.
type LifecycleState string

const (
	// LifecycleStarting is the application before it is ready.
	LifecycleStarting LifecycleState = "starting"
	// LifecycleInactive is it ready but not the one the user works in.
	LifecycleInactive LifecycleState = "inactive"
	// LifecycleActive is it ready and the one the user works in.
	LifecycleActive LifecycleState = "active"
)

// Lifecycle returns the latest activity state. A phone's backgrounding never
// means Quit: the OS may later suspend or terminate the process without a
// callback.
func (a *Application) Lifecycle() LifecycleState {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.lifecycle == "" {
		return LifecycleStarting
	}
	return a.lifecycle
}

// setLifecycle records s, and tells the listeners when it changed.
func (a *Application) setLifecycle(s LifecycleState) {
	a.mu.Lock()
	old := a.lifecycle
	if old == s {
		a.mu.Unlock()
		return
	}
	a.lifecycle = s
	a.mu.Unlock()
	fire1(&a.onLifecycleChanged, s)
}

// OnLifecycleChanged hears the application becoming active and inactive.
func (a *Application) OnLifecycleChanged(fn func(LifecycleState)) func() {
	return a.onLifecycleChanged.add(fn, false)
}
