//go:build !js

package mygo

import "testing"

// TestWindowsSupported checks the ordinary answer: a platform that can create
// windows says so, and the call works before any window exists (a host asks
// at startup to decide whether to build a UI at all).
func TestWindowsSupported(t *testing.T) {
	if !WindowsSupported() {
		t.Fatal("WindowsSupported = false on a platform that can create windows")
	}
}
