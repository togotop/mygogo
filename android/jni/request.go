//go:build android && cgo

package jni

// The request seam: a host asks MyGo to do something, and the answer comes
// back through HostEvent.
//
// It exists because the service layer is the app's code, not the framework's:
// MyGo does not know what a lamp is. The app registers a handler in its init,
// the host calls nativeRequest with a JSON message, and the handler runs on a
// goroutine of its own and answers with Emit.
//
// nativeRequest never runs the handler on the calling (host) thread: it queues
// and returns whether the message was taken, so a slow handler cannot freeze
// the host's UI. That is the same rule the surface exports follow (see jni.go).

/*
#include <jni.h>
*/
import "C"

import (
	"log"
	"sync"
)

// requestQueue is how many unanswered requests may wait. A host that floods the
// seam gets "busy" rather than unbounded memory.
const requestQueue = 8

var (
	requestMu sync.Mutex
	requestCh = make(chan string, requestQueue)
	requestOn bool
)

// Request registers the function that answers requests. It is called from an
// app's package init, before any host call arrives, and runs once: a second
// call replaces nothing and is ignored.
//
// The handler runs on a goroutine, one message at a time; it may block, but a
// slow handler delays every request behind it.
func Request(fn func(message string)) {
	if fn == nil {
		return
	}
	requestMu.Lock()
	if requestOn {
		requestMu.Unlock()
		return
	}
	requestOn = true
	requestMu.Unlock()
	go func() {
		for message := range requestCh {
			func() {
				// A panic in the app's handler must not take the host down
				// with it: answer nothing and keep serving.
				defer func() {
					if r := recover(); r != nil {
						log.Printf("mygo: request handler panicked: %v", r)
					}
				}()
				fn(message)
			}()
		}
	}()
}

// Emit sends one JSON message to the host: a reply to a request, or an event.
// It is safe from any goroutine; the host decides where it lands.
func Emit(message string) {
	theBackend.HostEvent(message)
}

// Java_org_mygo_Native_nativeRequest is nativeRequest: one JSON message from
// the host. It returns 1 when the message was taken and 0 when it was refused
// (no handler, or the queue is full), so the host can answer "busy" instead of
// waiting.
//
//export Java_org_mygo_Native_nativeRequest
func Java_org_mygo_Native_nativeRequest(env *C.JNIEnv, clazz C.jclass, json C.jbyteArray) C.jint {
	message := javaBytes(env, json)
	requestMu.Lock()
	on := requestOn
	requestMu.Unlock()
	if !on {
		androidLog("request: no handler registered")
		return 0
	}
	select {
	case requestCh <- message:
		return 1
	default:
		androidLog("request: queue full")
		return 0
	}
}
