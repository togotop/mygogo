//go:build android && cgo

package main

import "github.com/egoist/mygo/android/jni"

func init() { jni.Start(run) }
