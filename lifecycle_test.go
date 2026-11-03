package mygo

import (
	"slices"
	"testing"
)

// The application hears itself becoming active and inactive, once per change,
// and a phone's backgrounding is the inactive state.
func TestLifecycle(t *testing.T) {
	appHandler{}.DidResignActive()
	var got []LifecycleState
	off := App.OnLifecycleChanged(func(s LifecycleState) { got = append(got, s) })
	defer off()
	appHandler{}.DidBecomeActive()
	appHandler{}.DidBecomeActive()
	appHandler{}.DidResignActive()
	if want := []LifecycleState{LifecycleActive, LifecycleInactive}; !slices.Equal(got, want) {
		t.Fatalf("lifecycle changes: %v, want %v", got, want)
	}
	if App.Lifecycle() != LifecycleInactive {
		t.Errorf("lifecycle: %v, want inactive", App.Lifecycle())
	}
}
