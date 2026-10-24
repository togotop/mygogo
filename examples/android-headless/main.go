//go:build android && cgo

// Command android-headless is the smallest host of MyGo without a window: it
// proves an app can start from a Java host that owns the window itself (a game
// engine), which is what nativeStartServices exists for.
//
// Build it into the host with the same command the smoke app uses:
//
//	GOOS=android GOARCH=amd64 CGO_ENABLED=1 CC=... \
//	  go build -buildmode=c-shared -o android/app/src/main/jniLibs/x86_64/libmygo.so .
//
// inside examples/android-headless, then assemble with -Pheadless=true.
package main

import (
	"log"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/android/jni"
)

func main() {}

// run is the app's main. There is no surface: it never creates a window, and
// WindowsSupported says so.
// reply is the fixed answer to any request, standing in for a real service.
const reply = `{"v":1,"id":1,"ok":true,"result":{"version":"headless-demo"}}`

func run() {
	mygo.App.OnMemoryWarning(func(level int) {
		log.Printf("headless: memory warning level %d", level)
	})
	jni.Request(func(message string) {
		log.Printf("headless: request %s", message)
		jni.Emit(reply)
	})
	go func() {
		time.Sleep(500 * time.Millisecond)
		log.Printf("headless: windows supported = %v", mygo.WindowsSupported())
		for i := 1; ; i++ {
			time.Sleep(2 * time.Second)
			log.Printf("headless: tick %d", i)
		}
	}()
	if err := mygo.App.Run(); err != nil {
		log.Printf("headless: run: %v", err)
	}
}
