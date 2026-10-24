//go:build android && cgo

package backend

/*
#include <android/native_window.h>
#include <stdint.h>
#include <stdlib.h>

// mygo_sw_present copies one frame straight into the window's next buffer.
//
// This is the way to a screen without EGL, which is what a driver that gives
// us no usable context leaves: the content draws in memory and the frame is
// copied here instead of uploaded as a texture. ANativeWindow_lock is
// deprecated since API 30 in favour of hardware buffers, but a SurfaceView's
// window still has a queue to lock, and no other route exists without EGL.
//
// The window's buffer is RGBA in order while the frame is premultiplied BGRA,
// which is what package ui draws, so the channels are swapped row by row.
// Returns 0 when nothing was shown.
static int mygo_sw_present(ANativeWindow *w, const void *pix, int stride, int width, int height) {
	if (w == NULL || pix == NULL || width <= 0 || height <= 0) {
		return 0;
	}
	if (ANativeWindow_setBuffersGeometry(w, width, height, WINDOW_FORMAT_RGBA_8888) != 0) {
		return 0;
	}
	ANativeWindow_Buffer buf;
	if (ANativeWindow_lock(w, &buf, NULL) != 0) {
		return 0;
	}
	const uint8_t *src = (const uint8_t *)pix;
	uint8_t *dst = (uint8_t *)buf.bits;
	int rows = height < buf.height ? height : buf.height;
	int cols = width < buf.width ? width : buf.width;
	for (int y = 0; y < rows; y++) {
		const uint8_t *s = src + (size_t)y * stride;
		uint8_t *d = dst + (size_t)y * buf.stride * 4;
		for (int x = 0; x < cols; x++) {
			d[x * 4 + 0] = s[x * 4 + 2];
			d[x * 4 + 1] = s[x * 4 + 1];
			d[x * 4 + 2] = s[x * 4 + 0];
			d[x * 4 + 3] = s[x * 4 + 3];
		}
	}
	ANativeWindow_unlockAndPost(w);
	return 1;
}
*/
import "C"

import (
	"log"
	"unsafe"
)

// forceCPU keeps every frame on the CPU: no EGL context is made current, so
// the content draws in memory, and the frame is copied into the window's
// buffer instead of uploaded as a texture. It is the fallback for a device
// whose EGL cannot be used at all, and the way to tell a driver problem from
// anything else in a diagnostic build:
//
//	go build -ldflags '-X github.com/egoist/mygo/internal/android/backend.forceCPU=1'
var forceCPU = ""

// forcedCPU reports whether this build is the one that never touches EGL.
func forcedCPU() bool { return forceCPU == "1" }

// presentSoftware copies pix into the window, and reports whether it showed.
func (s *Surface) presentSoftware(pix []byte, stride, width, height int) bool {
	if len(pix) == 0 || s.window == nil {
		log.Printf("mygo/android: software present: %d bytes, window present %v", len(pix), s.window != nil)
		return false
	}
	ok := C.mygo_sw_present((*C.ANativeWindow)(s.window), unsafe.Pointer(&pix[0]),
		C.int(stride), C.int(width), C.int(height))
	if ok != 1 {
		log.Printf("mygo/android: software present refused %dx%d (stride %d, %d bytes)", width, height, stride, len(pix))
	}
	return ok == 1
}
