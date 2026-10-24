//go:build android && cgo

package main

import "github.com/egoist/mygo/android/jni"

// init registers the app's main with the Android host: the library's main is
// never called under -buildmode=c-shared, so this init, which runs when the
// runtime starts, is the only hook. jni.Start spawns run on the host's first
// surface callback.
func init() { jni.Start(run) }
