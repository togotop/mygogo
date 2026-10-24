# Video

MyGo draws every pixel itself: a scene of quads and glyphs goes through
`internal/gpu` to OpenGL, Metal or D3D11, and a window shows the result. There is
no video path today — `scene.Image` carries premultiplied RGBA (`Pix`), or a
`Scene` a renderer draws into a texture, and that is all.

This page evaluates the ways to play video inside that, for the app that needs
it (a smart-home control app: camera live view, doorbell clips, the odd local
file) and for the framework in general.

## What the app actually needs

| Use | Source | Format | Constraint |
| --- | --- | --- | --- |
| Camera live view | LAN camera | RTSP, sometimes ONVIF profiles | 1–2 s latency is fine; 4 s is not |
| Doorbell / event clip | server or cloud | HLS or MP4 | starts in under a second, seekable |
| Local file | the phone | MP4 / MKV | airplay to a TV, full screen |
| Ads / onboarding | bundled | MP4, silent | must not block the UI |

None of it needs DRM. All of it needs hardware decoding (a phone decoding 1080p
H.264 in software drains the battery and drops frames), and most of it needs to
sit *inside* a page — the camera view is a card in a room, not a full-screen
player.

## The three routes

### A. A native view on top of the canvas

The host puts the platform's own video view (Android `SurfaceView`/`TextureView`,
iOS `AVPlayerLayer`, Windows a child HWND, Linux a `GtkGLArea`) at the rectangle the
Go layout reports, above MyGo's drawing surface.

- ✅ Hardware decode, every format the platform supports, subtitles, HDR, and
  DRM if it ever matters (secure surfaces cannot be composited into a normal
  texture at all).
- ✅ Smallest amount of new code: a `ui.Video` element, a rect reported to the
  backend, and a player object per platform.
- ❌ The video is **above** the canvas: a MyGo-drawn dialog, menu or toast cannot
  cover it. Fixes are all ugly (hide the video while a sheet is open; draw the
  sheet in a second native window).
- ❌ Animating or clipping it (rounded corners, scroll-away in a list) needs the
  native view to follow the rect every frame, which looks fine at rest and tears
  during scroll on Android's `SurfaceView`.
- ❌ A second surface costs a full-screen composite on some GPUs, and on Android
  `SurfaceView` inside a scrolling list has a long history of flicker.

### B. Decode into a texture and draw it as an image

The player decodes into a platform texture (Android `SurfaceTexture` → OES
texture; iOS/macOS `CVPixelBuffer` → Metal texture; Windows a D3D11 texture;
Linux a dmabuf or a GL texture), and the existing renderer draws it wherever the
layout says, with the same clipping, transforms and z-order as anything else.

- ✅ One picture, one compositor: dialogs, rounded corners, scroll-away, PiP
  transitions all work because the video is just another quad.
- ✅ A/V sync is left to the player (ExoPlayer, AVPlayer, Media Foundation), not
  to us.
- ❌ Every backend needs a new texture source and, on Android, a second sampling
  path (`samplerExternalOES`), because an OES texture cannot be sampled by the
  ordinary shader.
- ❌ Frames cost a texture ping-pong per display refresh; on the CPU-fallback
  renderer (`Pix`) a read-back per frame is not viable above 480p.
- ❌ No DRM, ever: Widevine requires a secure surface, which cannot be sampled.

### C. Hand it to the system player

`ACTION_VIEW` on Android, `AVPlayerViewController` on iOS, the default browser
handler on desktop.

- ✅ Nothing to write, subtitles and PiP for free.
- ❌ Leaves the app: no camera stream, no embedded card, no styling. Only good
  for "open this file".

## Per-platform players

| Platform | Player | License | Notes |
| --- | --- | --- | --- |
| Android | **Media3 / ExoPlayer** | Apache-2.0 | MP4, HLS, DASH, RTSP, SmoothStreaming; hardware decode; ~1.5 MB after R8 |
| Android | platform `MediaPlayer` | — | zero dependency, but RTSP support is patchy and the API is a dead end |
| Android | libVLC | LGPL-2.1 | every codec, 20 MB+, awkward JNI lifecycle |
| iOS / macOS | **AVPlayer** | — | HLS, MP4; RTSP only through a third-party (VLCKit, or a proxy) |
| Windows | **Media Foundation** | — | MP4/HLS; the API is verbose but cgo-friendly |
| Windows | libVLC | LGPL-2.1 | the fallback for exotic containers |
| Linux | **GStreamer** | LGPL-2.0 | MP4/HLS/RTSP via plugins; the distro usually has it |
| Linux | libmpv | GPL-2.0+ (or LGPL build) | the best RTSP behaviour, the heavier dependency |
| Web / wasm | `<video>` | — | the browser does everything, including HLS on Safari |

RTSP is the awkward one: Android's Media3 supports it, AVPlayer does not, and
GStreamer needs `gst-rtsp-server`'s client plugin present. A camera feature that
must work on iOS either ships VLCKit or speaks WebRTC instead — and WebRTC means
a signalling stack, roughly `pion/webrtc` in Go plus the platform decoder, which
is a project of its own, not a wrapper.

## Recommendation

**Android first, route B, with route A as the escape hatch.**

1. **`ui.Video`** element and a Go API (`mygo.Video.Play(VideoOptions)` returning
   a handle with `Pause`, `Seek`, `SetVolume`, `Close`), shaped like the audio
   package: the element reports its rect, the player is told to draw there.
2. **Android backend**: Media3 ExoPlayer renders into a `SurfaceTexture`, the GL
   renderer samples it through a new external-texture shader path, and
   `scene.Image` grows a texture source (a small interface next to `Scene`).
   Audio goes through ExoPlayer's own `AudioTrack`, not `audio` — A/V sync is
   the player's job and mixing two clocks is how lip-sync bugs start.
3. **Route A for what route B cannot do**: DRM, and platforms where the texture
   plumbing is a bigger job than the feature (Windows child HWND, Linux GTK
   widget). The `ui.Video` element stays the same; only the backend differs.
4. **iOS / macOS** later, with `AVPlayer` and a `CVPixelBuffer` → Metal texture;
   their layout code already reports rects, so the element does not change.
5. **Web** is free: `<video>` in the DOM under a canvas, or the `<video>` element
   itself when the page uses DOM rendering.

The order matters: Android is the platform the app ships on, the renderer there
already has the hooks (`imageTexture`), and Media3 does the hard parts (demux,
decode, sync, buffering, HLS/RTSP). Everything else is a port of the same shape.

## What it costs, per platform

| Piece | Android | iOS/macOS | Windows | Linux |
| --- | --- | --- | --- | --- |
| Player integration | Media3 | AVPlayer | Media Foundation | GStreamer |
| Texture into renderer | OES + shader branch | CVPixelBuffer → Metal | D3D11 texture | GL texture / dmabuf |
| Frame pacing | `onFrameAvailable` → redraw | display link | `IMFSample` | `appsink` |
| Effort | the reference implementation | a port of it | a port, rougher | a port, rougher |

## Verification

- A 1080p30 H.264 MP4 in a scrolling list: no dropped frames on the emulator's
  software decoder is not a test — use a device with a hardware decoder.
- A dialog over a playing video (route B must cover it, route A must not be
  expected to).
- Rotate while playing, background and resume, and a 10-minute HLS stream that
  rebuffers — the state machine is where players usually leak.
- Camera RTSP at 1080p: measure time from tap to first frame, and lip-sync on a
  clip with speech.

## Open questions

- Does the camera speak RTSP at all, or WebRTC? If WebRTC, this page's plan is the
  wrong shape and the work starts at `pion/webrtc` instead.
- Is DRM ever needed? If yes, route A stops being a fallback and becomes the
  requirement on every platform.
- Is audio ever the app's own (an intercom talking back)? That changes the audio
  routing decision above.
