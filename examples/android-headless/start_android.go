//go:build android && cgo

package main

import "github.com/egoist/mygo/android/jni"

// init runs when the library is loaded, before any JNI call: the host's
// javaStartServices (nativeStartServices) then spawns run on a goroutine.
func init() { jni.Start(run) }
