//go:build android && cgo

package main

import "github.com/egoist/mygo/android/jni"

// init registers run with the Android host: under -buildmode=c-shared the
// library's main is never called, so this init, which runs when the runtime
// starts, is the only hook. jni.Start spawns run on the host's first surface.
func init() { jni.Start(run) }
