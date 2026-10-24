//go:build android && cgo

package backend

import (
	"log"
	"unsafe"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/transfer"
)

// presentFailureLimit is how many consecutive EGL presents must fail before
// the window buffer takes over for the rest of the process.
const presentFailureLimit = 3

// Surface is the platform.Surface of the Activity's window.
//
// Android has no mygo window to hang a surface off: the Activity owns the
// window and its SurfaceView hands us a Surface, which arrives through
// nativeOnSurfaceCreated and whose size arrives through
// nativeOnSurfaceChanged. So this Surface exists from the first
// surfaceCreated and is driven by the JNI callbacks rather than by a
// platform.Window (see Backend).
type Surface struct {
	b   *Backend
	egl *eglState

	// present holds the GL objects that turn a frame drawn in memory into what
	// the display shows; they belong to the EGL context, so they outlive every
	// surface change (see glpresent.go).
	present presenter

	// window is the ANativeWindow* from ANativeWindow_fromSurface, which
	// the backend owns and releases after the EGLSurface is gone.
	window unsafe.Pointer

	// width and height are the surface's size in device pixels; dpi is
	// its density in dots per inch, where 160 (mdpi) is Android's
	// baseline.
	width, height, dpi int

	// software is set when EGL presenting failed often enough to give up on
	// it for the rest of the process: the window buffer still shows a frame.
	// presentFailures counts the consecutive failures that decide it, and a
	// new surface resets the count (a dying surface fails a swap too).
	software        bool
	presentFailures int
	// gpuBroken hides the native objects from the ui layer after the GPU's
	// frames stopped reaching the display: it then draws in memory, which is
	// the path PresentPixels can show through the window buffer.
	gpuBroken bool
}

// Native returns the native objects a GPU renderer draws into.
//
// platform.SurfaceNative has a field per desktop platform — Windows' HWND,
// macOS's View and Layer, Linux's Widget and GLArea — and none for
// Android, where the object is an ANativeWindow* the Activity gave us
// rather than one mygo created. Adding a field would change a struct every
// backend reads, so the ANativeWindow* travels in View (as a uintptr), the
// field whose meaning — the object a renderer records drawing into — is
// the closest, and the choice is repeated here in case a dedicated field is
// wanted later.
func (s *Surface) Native() platform.SurfaceNative {
	if s.gpuBroken {
		// No native objects: the ui layer makes no GPU renderer and draws in
		// memory, which is what a device whose GPU frames never arrive needs.
		return platform.SurfaceNative{}
	}
	return platform.SurfaceNative{View: uintptr(s.window), Present: s}
}

// Size returns the drawing area in DIPs and how many device pixels a DIP
// is. Android reports the surface in device pixels and reports the density
// in dots per inch, so a DIP is dpi/160 device pixels.
func (s *Surface) Size() (width, height, scale float64) {
	scale = float64(s.dpi) / 160
	if scale <= 0 {
		scale = 1
	}
	return float64(s.width) / scale, float64(s.height) / scale, scale
}

// SetSwapInterval tells eglSwapBuffers how many vsyncs to wait for. 1 is the
// default; 0 makes it return as soon as the buffer is handed over, which is
// how the frame's own cost is read apart from its pacing (see
// eglState.setSwapInterval).
//
// It is platform.SwapIntervalSurface, reached by an optional type assertion
// rather than through the Surface interface, because only EGL has an
// interval to set.
func (s *Surface) SetSwapInterval(n int) {
	if forcedCPU() || s.egl == nil {
		return
	}
	s.egl.setSwapInterval(n)
}

// PresentPixels shows a frame drawn in memory: premultiplied BGRA rows of
// stride bytes, width×height device pixels.
//
// The rows become a texture the presenter draws (see glpresent.go), so the
// frame the ui package rendered in memory is what the display shows. A caller
// that hands over no usable frame gets the placeholder fill instead, which
// keeps the surface from showing whatever the driver last left in the buffer.
func (s *Surface) PresentPixels(pix []byte, stride, width, height int) {
	if forcedCPU() || s.software || s.egl == nil || !s.egl.makeCurrent() {
		// No usable EGL, so the frame goes into the window's buffer instead
		// of a texture: slower, but a screen rather than none. The landmark
		// below needs the context, so a frame that is not there yet leaves
		// what the window already shows.
		if pix != nil {
			s.presentSoftware(pix, stride, width, height)
		}
		return
	}
	if width > 0 && height > 0 {
		s.egl.viewport(width, height)
	}
	drawn := s.present.draw(pix, stride, width, height)
	if !drawn {
		// No frame to show: the app has not drawn one, or has not started.
		//
		// A window with a background color gets that color, which is what a
		// person sees while the app starts: the green and red halves below
		// are a landmark for a window that has none, where "the surface is
		// alive and nothing has drawn yet" has to be readable by eye. A
		// shipping app paints its own color instead of showing a test
		// pattern for the few hundred milliseconds Go takes to start.
		if bg := s.b.background(); bg != nil {
			s.egl.clear(float32(bg.R)/255, float32(bg.G)/255, float32(bg.B)/255, 1)
		} else {
			s.egl.clear(0.85, 0.15, 0.15, 1)
			if width > 0 && height > 0 {
				s.egl.scissor(0, 0, width/2, height)
				s.egl.clear(0.15, 0.75, 0.25, 1)
				s.egl.scissorOff()
			}
		}
	}
	swapped := s.egl.swap()
	if drawn && swapped {
		s.presentFailures = 0
		return
	}
	// A texture path that stopped working, or a frame that drew but never
	// reached the display, is a driver that stopped answering. After a few in
	// a row the window buffer is the only way to a screen: the app keeps its
	// content and loses the GPU's copy, which is what it already does on a
	// device with no usable EGL at all.
	if pix != nil {
		s.presentFailures++
		if s.presentFailures >= presentFailureLimit {
			s.software = true
			log.Printf("mygo/android: presenting through EGL failed %d times, using the window buffer", s.presentFailures)
			// The EGLSurface owns the window's buffer queue: the window can be
			// locked only once EGL lets go of it. The context stays, so a
			// later surface change can try EGL again.
			s.egl.destroyWindow()
			if !s.presentSoftware(pix, stride, width, height) {
				log.Printf("mygo/android: the window buffer refused the frame too")
			}
		}
	}
}

// RefreshRate returns how many times a second the display showing this
// surface refreshes, from the rate the Activity last reported (see
// SetDisplayRefreshRate), or 0 when nothing has.
//
// 🔴 It is not the panel's maximum. Android picks a mode per window and
// moves between them — an idle page on a 120 Hz panel is shown at 60 — so
// this is the rate the window is being driven at now, which is the number
// the engine's pacing needs. The maximum is asked for separately, once, in
// MygoView.requestMaxRefreshRate.
func (s *Surface) RefreshRate() float64 { return DisplayRefreshRate() }

// SetCursor does nothing: touch devices show no pointer over the surface.
func (s *Surface) SetCursor(platform.Cursor) {}

// SetTextInput hands the surface's text-input state to the Android IME state
// machine, which is what answers an input method's queries and raises the
// soft keyboard (see textinput.go).
func (s *Surface) SetTextInput(t platform.TextInputState) {
	theIme.SetTextInput(t)
}

// UpdateAccessibility pushes the tree to the Java node provider, which is what
// TalkBack reads (see platform.go and MygoPlatform.kt).
func (s *Surface) UpdateAccessibility(t *platform.AccessTree) { s.b.UpdateAccessibility(t) }

// StartDataDrag does nothing: native drag-and-drop does not exist here.
func (s *Surface) StartDataDrag(platform.DragRequest) {}

// CancelDataDrag does nothing: no drag can have been started.
func (s *Surface) CancelDataDrag() {}

// SetDropFormats does nothing: files are not dropped on a touch surface.
func (s *Surface) SetDropFormats([]transfer.Format) {}

// NewWebView reports that the surface hosts no web view: Android's page is
// WebView, a view of the Activity, not a web view under this surface.
func (s *Surface) NewWebView(*platform.WindowOptions, platform.WindowHandler) (platform.WebView, error) {
	return nil, platform.ErrUnsupported
}

// PlaceWebViews does nothing: there are no web views to place.
func (s *Surface) PlaceWebViews([]platform.WebViewPlacement) {}

// Surface must satisfy the platform contract.
var _ platform.Surface = (*Surface)(nil)
