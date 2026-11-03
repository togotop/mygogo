# Phones and tablets

The same MyGo app runs on iPhones and iPads, and on Android phones and
tablets: windows of web pages show in the system's web view (WKWebView on
iOS, Android's WebView), windows of native UI draw with the GPU (Metal on
iOS, OpenGL ES on Android), and the app's Go code, its bound services and
its events are the same. `mygo dev` runs the app on a simulator, an
emulator or a device as you change it; `mygo build` makes what the App
Store and Google Play take.

```sh
mygo doctor                    # what is there, and what is missing
mygo devices                   # what -device takes
mygo dev -platform ios         # the booted iOS simulator
mygo dev -platform android     # the device or emulator adb reaches
mygo build -platform ios       # a signed .ipa
mygo build -platform android   # an .apk and an .aab
mygo publish -platform ios     # the .ipa to App Store Connect
mygo publish -platform android # the .aab to a track of Google Play
```

## What you need

- **iOS**: a Mac with Xcode. The Go code links with Xcode's clang (cgo,
  which Go needs to link for iOS), and the CLI compiles the app's Swift
  host, its icon and its bundle, and signs it, without an Xcode project.
  Devices and the App Store need an Apple developer team and a
  provisioning profile for the app's identifier, which Xcode makes
  (Settings › Accounts) or developer.apple.com.
- **Android**: Android Studio with the SDK and the NDK (SDK Manager › SDK
  Tools › NDK), on any system. The Go code is a shared library for each
  ABI, linked by the NDK's clang, and Gradle builds the app; the CLI finds
  the SDK in its usual place or `ANDROID_HOME`, the NDK in the SDK or
  `ANDROID_NDK_HOME`, and Android Studio's JDK or `JAVA_HOME`.

Desktop builds still need neither cgo nor these tools.

`mygo doctor` checks them. On a Mac it looks at Xcode and its command line
tools, the iOS SDK, the simulators, the signing identities and the
provisioning profiles; on every system at the Android SDK, adb, the NDK, the
JDK, the emulators and the devices connected. What is missing there is a
warning that says how to get it, never a failure: an app for desktops needs
none of it.

## Devices

`mygo devices` lists what `-device` takes, by name and ID: iOS simulators
with their runtimes, iOS devices, Android devices and emulators, and the
virtual devices (AVDs) of emulators that are not running. `-json` prints it
for scripts.

```
NAME           ID                                    PLATFORM    KIND       STATE
iPhone 18 Pro  629AC4ED-EFB6-4090-9143-E4B57B3220DA  iOS 27.0    simulator  booted
Jane's iPhone  00008140-000E55192283801C             iOS 27.0.1  device     connected
Pixel_9        emulator-5554                         Android 17  emulator   booted
Tablet         Tablet                                Android     emulator   shutdown
```

It asks `xcrun simctl`, `xcrun devicectl` (Xcode 15 and later), `adb` and
`emulator`, and leaves out what it cannot ask.

## Developing

`mygo dev -platform ios` builds the app for the simulator, installs it on
the one booted (or `-device`, a simulator's name or UDID, which it boots)
and launches it, the app's output in the console. Each change of the Go
code rebuilds and relaunches it in a few seconds. A frontend's `devUrl`
works as on the desktop: the simulator reaches the Mac's dev server.

`mygo dev -platform android` does the same with the device or emulator
`adb` reaches (or `-device`, its serial), which reaches the dev server
through `adb reverse`, at the same URL. Without `devUrl`, the app embeds
`frontendDist`, as a device cannot read the computer's disk.

A change of the Go code takes about two seconds: the host's Swift stays
compiled and the new executable goes into the installed app on iOS, and on
Android the new library goes to the debuggable app without Gradle. A change of
the host, a plugin, the resources or the configuration builds and installs the
whole app again.

Go can't swap code in a running program, so a change restarts the app, and the
app comes back where it was. Scroll offsets, where a `ui.Router` is and what
widgets keep with `ui.Local` return by themselves. One line brings back the app's
own values, unexported fields and all, with no tags or methods on its types:

```go
a := &app{tab: 0}
mygo.KeepState(a)
```

It also brings the app back as the system ended it in the background. It keeps
data: functions, channels, interfaces and pointers to other packages' types stay
as the program makes them, and a field whose type the change altered stays as
made. `App.OnSaveState` and `App.RestoredState` are there for more control. All
of it works under `mygo dev` on desktops too.

The development app is "<name> Dev" with the identifier
"<identifier>.dev", as on the desktop, so it installs beside the release.

### On an iPhone or iPad

`mygo dev -platform ios -device <name or UDID>` also takes a device, which
needs Developer Mode on (Settings › Privacy & Security). The app builds for
the device and signs with a development provisioning profile: the installed
one that best fits "<identifier>.dev", usually the wildcard profile Xcode
keeps, whose certificate is in the keychain and which lists the device. A
profile `ios.provisioningProfile` names is for `ios.distribution`, so it signs
development builds only when that is `development`.

`xcrun devicectl` installs and launches it, the app's output in the console;
an Xcode whose `devicectl` has no `--console` still runs the app, without its
output. A device is not on the Mac's network stack: it loads a `devUrl` on
`localhost` at the Mac's address on the network, which the dev server must
listen on (`vite --host`), and development builds allow local networking and
say why for the system's prompt.

A wildcard profile cannot have Associated Domains: with one, the development
build leaves `ios.associatedDomains` out and says so.

### Android emulators

`-device` also takes the name of an AVD. One that is not running starts,
apart from `mygo dev`, which waits for its system to finish booting; it keeps
running after `mygo dev` ends, as starting it again is slow.

## Building

```sh
mygo build -platform ios             # dist/ios-arm64/<slug>-<version>.ipa, signed
mygo build -platform ios/simulator   # dist/ios-simulator/<name>.app
mygo build -platform android         # dist/android/<slug>-<version>.apk and .aab
mygo build -platform android/arm64   # one ABI: arm64, arm or amd64
```

The `.ipa` is signed with the installed provisioning profile that best
fits the app's identifier and `ios.distribution` (`app-store` by default,
which is also TestFlight's), whose certificate is in the keychain; it
carries the privacy manifest App Store Connect asks for, made from what the
app's code uses, unless the project has its own
`ios/PrivacyInfo.xcprivacy`. The CLI refuses a build that names Apple's private methods, which App
Review turns down.

The `.aab` is what Google Play takes, the `.apk` what other stores and
devices install. Release builds are signed with `android.keystore`, its
passwords from `MYGO_ANDROID_KEYSTORE_PASSWORD` and
`MYGO_ANDROID_KEY_PASSWORD`, or else with the debug key, which Google Play
refuses.

The configuration's `ios` and `android` sections set the rest (see
[Configuration](configuration.md)):

```ts
export default defineConfig({
  name: "Todo",
  identifier: "dev.example.todo",
  ios: { team: "ABCDE12345", infoPlist: { NSCameraUsageDescription: "Scan receipts." } },
  android: { permissions: ["CAMERA"], keystore: "upload.jks", keyAlias: "upload" },
});
```

## Publishing

`mygo publish` uploads what `mygo build` wrote for this version, or `-file`.

```sh
mygo build -platform ios && mygo publish -platform ios
mygo build -platform android && mygo publish -platform android -track beta
```

**iOS** uploads the `.ipa` to App Store Connect with `xcrun altool`, where it
shows in TestFlight once processed. It takes an API key of the team (App Store
Connect › Users and Access › Integrations), which never goes on the command
line:

| Variable | |
|---|---|
| `MYGO_ASC_KEY_ID` | the key's ID |
| `MYGO_ASC_ISSUER_ID` | the issuer's ID |
| `MYGO_ASC_KEY` | the path of its `.p8` |

**Android** uploads the `.aab` through Google Play's Android Publisher API:
an edit, its bundle, a release on the track, then the commit, or the edit
deleted if anything fails. `MYGO_PLAY_SERVICE_ACCOUNT` holds the path of a
service account's JSON key, or the JSON itself, for secrets of CI; the account
needs access to the app in Play Console. `-track` is `internal` by default, or
`alpha`, `beta`, `production` or a closed testing track's name. Google Play
takes the first upload of an app in its console only, and an app it has not
reviewed yet takes releases only with `-status draft`.

The app's `identifier` is the package it uploads to.

## Links, icons and launch screens

```ts
export default defineConfig({
  ios: {
    associatedDomains: ["example.com"],
    launchScreen: { background: "#102030", image: "resources/logo.png" },
    backgroundModes: ["audio", "remote-notification"],
  },
  android: {
    appLinks: ["example.com"],
    adaptiveIcon: { foreground: "resources/icon-foreground.png", background: "#102030" },
    splash: "#102030",
  },
});
```

**Links**: https URLs of your domains open the app, and reach
`App.OnOpenURL`. `ios.associatedDomains` signs the app with
`applinks:example.com` (a value with its own service, as
`webcredentials:example.com`, stays as it is); the app ID needs the capability
Associated Domains, and the host an `apple-app-site-association` file.
`android.appLinks` adds a verified intent filter per host (`*.example.com`
takes its subdomains), and each host serves
`/.well-known/assetlinks.json` naming the app and its signing key.

**Android's icon**: `android.adaptiveIcon` is the launcher icon of Android 8.0
and later, a PNG of 432×432 or more over a color. Launchers crop to the middle
72/108 of it and may move it, so keep what matters there. `icon` stays what
older systems show.

**Launch screens**: `ios.launchScreen` is a color, an image centered on it, or
both, with no storyboard: the CLI compiles them into the app's asset catalog
and names them in `UILaunchScreen`. The image is a PNG at the size of a 3×
screen, so a logo of 600×600 pixels takes 200×200 points. `android.splash` is
the color of the screen the system shows while the app starts, under the
app's icon.

**Background modes**: `ios.backgroundModes` are the app's
`UIBackgroundModes`, with those its plugins ask for.

## Tabs and stacks

Phones move through an app with the system's bar of tabs at the bottom
and stacks of screens, each with a bar at the top holding its title and a
back button. `mygo.NewTabs` shows a bar of tabs (UIKit's on iOS, Material's
bottom navigation on Android), and `Window.Push` shows a window over
another, on its tab's stack:

```go
mygo.App.WhenReady(func() {
	list := mygo.NewWindow(mygo.WindowOptions{Title: "Fruits", Content: ui.View(app.list), Hidden: true})
	web := mygo.NewWindow(mygo.WindowOptions{Title: "Web", URL: "/", Hidden: true})
	tabs, _ := mygo.NewTabs(
		mygo.TabItem{Title: "Fruits", Symbol: "leaf", Icon: leafPNG, Window: list},
		mygo.TabItem{Title: "Web", Symbol: "globe", Icon: globePNG, Window: web},
	)
	tabs.SetBadge(1, "3")
	app.open = func(fruit string) {
		list.Push(mygo.NewWindow(mygo.WindowOptions{Title: fruit, Content: ui.View(app.detail(fruit)), Hidden: true}))
	}
})
```

A tab names an SF Symbol for iOS and gives a PNG for Android (and for iOS
without a symbol), drawn as a template in the bar's colors. The back
button, iOS's swipe from the left edge and Android's back pop a pushed
window, which closes it; `Window.Pop` does too. On iOS the bars float
over native UI, whose lists scroll on beneath the tab bar's glass; on
Android the bars take the color of the window's background. On desktops
the windows are windows: `Tabs.Select` shows one, and a pushed window
shows as one. `examples/tabs` shows both, with a page in one of its tabs.

On iPad a window can be a window of the system's own, beside the app's others, with
`"ios": {"multipleWindows": true}` in `mygo.json`:

```go
mygo.NewWindow(mygo.WindowOptions{Title: "Draft", OwnScene: true, Content: ui.View(draft)}).Show()
```

Elsewhere it's a window as any: on a phone, over the one before. Android's tablets
split the screen with other apps by themselves.

## Fingers

Native UI takes fingers as the systems' own toolkits do:

- A tap is a click. A finger reaches what it nearly touches, by 14 DIPs: a
  slider's thumb is narrower than a fingertip.
- A finger that moves locks to the way it went, and to the innermost container
  that scrolls that way: a row of photos in a list down the screen takes what
  goes across, and leaves what goes down to the list. It flings the content on
  as it lifts, and stops a fling as it lands, tapping nothing.
- Past the content's ends, the content follows less and less far and springs
  back, a long way on iOS and less on Android. A scroll indicator shows as the
  content moves.
- What drags along a way takes what goes that way: a sideways `Draggable` in a
  list, even with buttons inside it, leaves what goes down to the list.
- Held still for half a second, a finger long-presses: `Element.LongPressed`,
  a context menu, a word of text, or what drags a value, as a list's rows that
  `Reorder` moves. The device gives a tick.
- Two fingers pinch and turn (`InputEvent`'s `InputZoom`, with `Rotation`).
- From the left edge on iOS, a finger takes a `ui.Router`'s page back, following
  it, as UIKit's stacks do. A tap on iOS's status bar sends what scrolls back to
  its top.

`Element.PullToRefresh` lets a finger pull a scroll container past its top:

```go
list := ui.Scroll(c).Fill()
if list.PullToRefresh(&app.refresh) {
	go func() {
		items := fetch()
		win.Update(func() { app.items, app.refresh.Refreshing = items, false })
	}()
}
```

## Text

The keyboard edits text through its input's `TextInputClient`, with the system's
suggestions and input methods. Elements say what they hold, which chooses the
keyboard:

```go
ui.TextInput(c, &app.email).
	Keyboard(ui.KeyboardEmail).ReturnKey(ui.ReturnNext).
	Capitalize(ui.CapitalizeNone).Autocorrect(false).Autofill(ui.AutofillEmail)
```

`ReturnNext` moves the focus on; `Password` is a secure entry. As the keyboard
comes up it shows the input in use. Holding a word chooses it, with the system's
menu of what to do with it (`UIEditMenuInteraction` and Android's floating
toolbar); the handles at its ends drag them, with the system's magnifier, drawn
as each system's. Tapping the caret shows the menu too.

## Widgets of phones

Package `ui/mobile` is what phones' interfaces are made of and desktops' aren't,
looking as each system's, on `ui`'s public API. Apps that don't import it don't
pay for it.

| | |
|---|---|
| `Sheet`, `ActionSheet` | Panels coming up from the bottom, with detents, which a finger drags away |
| `Drawer` | A panel sliding in from the side |
| `PageView`, `PageDots` | Pages a finger swipes between, built as they come near |
| `SwipeRow` | A row a finger swipes aside to reveal actions |
| `Wheel`, `DatePicker`, `TimePicker` | Wheels that spin, with a tick as the choice changes |
| `TopBar`, `BottomBar`, `FloatingButton` | Bars drawn by the app, for one window that routes within it |
| `ListRow`, `Section` | Rows and groups of them, with a press's look |
| `Ink` | Material's ripple |
| `Switch` | The system's switch |
| `Zoomable` | Content that fingers pinch, turn and drag, as a photo |

`mobile.SetLook` chooses a design whatever the system. `examples/mobile` shows
them all.

Pictures from the network take `ui/netimage`, in place of `ui.Image`:

```go
netimage.Image(c, user.AvatarURL, netimage.Options{MaxSide: 240}).Size(80, 80).Radius(40)
```

A placeholder holds their place, and they fade in once they come. They stay in
memory while there is room and on disk across runs, as their servers' caching
headers say, and show offline. What's on screen downloads before what a fling
left behind, which doesn't download at all if it's gone by its turn. Desktops
and browsers take it too.

## The device

| | |
|---|---|
| `mygo.Haptics` | The feel of a selection, an impact or how a task ended |
| `mygo.SystemBars` | The color of the icons of the system's bars, and hiding them |
| `mygo.SetOrientations` | How the screen may turn |
| `mygo.Share` | The system's share sheet of text, a link and files |
| `mygo.Network` | Whether it is online, and whether that costs |
| `mygo.Power.Battery` | The battery's level, and the low power mode |
| `mygo.SetBarItems` | The buttons of a window's system navigation bar |
| `Context.SafeArea` | What covers the window's edges, to draw in the `Overlay` |

They do nothing on desktops, or report `errors.ErrUnsupported`.

## Official plugins

What needs permissions or a large dependency is a plugin, which an app has only
if it imports it, with its usage descriptions and permissions added to the app:

| Package (`plugins/...`) | What it does |
|---|---|
| `maps` | A map in native UI, with pins: MapKit, and osmdroid with OpenStreetMap's tiles, which needs no key |
| `location` | Where the device is, and following it, through Core Location and LocationManager |
| `imagepicker` | The system's picker of photos and videos, which needs no permission, and its camera screen |
| `camera` | A live preview in native UI, and photos: AVCaptureSession and CameraX |
| `videoplayer` | Video in native UI: AVPlayer and MediaPlayer |
| `biometrics` | Face ID, Touch ID, fingerprints, the passcode |
| `sensors` | The accelerometer, the gyroscope and the magnetometer |
| `push` | APNs and Firebase tokens, messages and tapped notifications (iOS devices need the Push Notifications capability in the profile, Firebase on Android a project) |
| `purchases` | In-app purchases and subscriptions: StoreKit 2 and Google Play Billing |
| `permissionflow` | Whether the app may use the camera, the microphone, photos, contacts…, asking, and the system's settings |
| `preferences` | Small values kept between runs, on every platform |
| `sqlite` | SQLite with full-text search, 64-bit, compiled into the app |
| `background` | Go code that runs while the app isn't open, as the system finds the time: BGTaskScheduler and WorkManager |
| `bluetooth` | Bluetooth LE: scanning, GATT reads, writes and notifications (Android) |
| `nfc` | The tags a phone is held against, written to and spoken to in their own protocol (Android) |
| `scanner` | A barcode or QR code read from a photo the system's camera takes, with no permission of the app (Android) |
| `speech` | Text said aloud with the device's voice (Android) |
| `tile` | The app's tile in the quick settings, whose presses come back to Go (Android) |
| `widget` | The app's widget on the home screen, whose presses come back to Go (Android) |

## Writing a plugin

A plugin with native code is a Go package beside its Swift and Kotlin, which its
`mygo-plugin.json` names. `mygo build` finds it among the packages the app
imports, compiles its code into the host and registers it:

```json
{
  "mobile": {
    "id": "torch",
    "ios": {
      "sources": ["ios/*.swift"],
      "class": "TorchPlugin",
      "frameworks": ["AVFoundation"],
      "infoPlist": {"NSCameraUsageDescription": "Lights the torch."}
    },
    "android": {
      "sources": ["android/*.kt"],
      "class": "TorchPlugin",
      "permissions": ["CAMERA"],
      "dependencies": ["androidx.camera:camera-core:1.6.2"],
      "application": ["<service android:name=\"dev.mygo.TorchService\" />"]
    }
  }
}
```

`resources` names directories of `res/` files the plugin brings: layouts, drawables
and XML, which land in the host's res, so that its Kotlin's `dev.mygo.R` sees
them and the app's resources merge with them. `manifest` names XML the app's
manifest takes beside `<application>`, as the `<queries>` entry Android 11 and
later need to reach a system service.

Go calls it with JSON, and hears its events:

```go
func On(ctx context.Context, on bool) error {
	return mygo.Native("torch").Call(ctx, "set", map[string]bool{"on": on}, nil)
}
```

The Swift goes in the host's module, the Kotlin in package `dev.mygo`, on the UI
thread. They answer once, at any time, and `PluginHost.emit` sends events
(`mygo.Native("torch").On`):

```swift
final class TorchPlugin: MyGoPlugin {
    func call(_ method: String, _ args: [String: Any], _ reply: @escaping PluginReply) {
        switch method {
        case "set": reply(.success(nil))
        default: reply(.failure(PluginFailure("NoMethod", method)))
        }
    }
}
```

```kotlin
package dev.mygo

class TorchPlugin : MyGoPlugin {
    override fun call(method: String, args: JSONObject, reply: PluginReply) {
        when (method) {
            "set" -> reply.success(null)
            else -> reply.failure(PluginFailure("NoMethod", method))
        }
    }
}
```

`PluginHost` has what they need of the host: on iOS the `presenter` to present
over and `applicationEvent` for what reaches the app's delegate; on Android the
`activity`, `startForResult` and `requestPermissions`. An error's code comes to
Go as a `*mygo.NativeError`.

### C code

A plugin's C compiles with the app, with the compiler phones need anyway, once
per target (the user's cache keeps it). The sqlite plugin brings SQLite this way:

```json
{"mobile": {"id": "sqlite", "native": {
  "name": "mygo-sqlite3",
  "archive": {"url": "https://www.sqlite.org/...zip", "sha256": "...", "files": ["sqlite3.c", "sqlite3.h"]},
  "sources": ["native/sqlite.c"],
  "cflags": ["-DSQLITE_THREADSAFE=1"],
  "ldflags": ["-lm"],
  "abis": ["arm64-v8a", "x86_64"]
}}}
```

iOS gets a framework inside the bundle, signed with the app, and Android a
library of the APK. `mygo.NativeLibrary(name)` is what to give dlopen, through
[purego](https://github.com/ebitengine/purego) as on desktops: a plugin needs
no cgo. A package may name libraries for desktops too, under `libraries`; on
phones a library it compiles takes their place.

### Native views

`PluginHost.registerView` registers a type of view, in the plugin's initializer,
which `mygo.NewPluginView` makes for a window showing native UI, and `ui.WebView`
places. It shows under the content, in a hole of its frames, as a web view does:
what the view paints after it shows over it, and fingers on that stay the
content's. The view's `ID` names it in calls about it. `plugins/videoplayer` is
a short example, for a map or anything else the system draws.

## Testing

Unit tests run views with a `ui.Tester`, whose clock they advance, with
`TouchDown`, `TouchMove`, `TouchUp` and `SetInsets`. Package `ui/golden` compares
what it draws with images beside the tests:

```go
func TestCard(t *testing.T) {
	golden.Match(t, ui.NewTester(card, 320, 200), "testdata/card.png")
}
```

Run with `MYGO_UPDATE_GOLDEN=1` to write them, on the platform that runs the
tests: text differs from one to another.

`MYGO_FRAME_STATS=1` logs frames slower than 8 ms, with what took the time, also
on phones: `SIMCTL_CHILD_MYGO_FRAME_STATS=1` for a simulator, and on Android
`adb shell am start ... --es mygo.env MYGO_FRAME_STATS=1`.

End-to-end tests drive the running app from another process, on simulators and
devices alike, with [the automation plugin](plugins/automation.md#on-phones): they
read the elements on screen, act on them, send fingers and text, capture the
screen and wait for it to settle.

Tap with four fingers to open the inspector of native UI in a development build:
along the bottom of a phone, with the elements, their styles, frame times and
issues, as F12 opens it on a desktop.

## How apps behave on phones

- **One screen**: the window shown last fills the screen, above those
  shown before; hiding or closing it shows the one under it. Window sizes,
  positions and title bars have no say. `Window.Bounds` is the screen's.
- **Native UI lays out inside the safe area**: the system's bars, the
  display's cutout and, while it shows, the keyboard, while the window's
  background fills the edges. Pages lay out inside the bars, and above the
  keyboard on Android.
- **Touch** is below ([Fingers](#fingers)); `Tester.TouchDown`, `TouchMove`
  and `TouchUp` test views with a finger.
- **The keyboard** shows while an element of native UI takes text, with
  the system's suggestions and input methods (composition included), and
  a hardware keyboard's keys work as on the desktop ([Text](#text)).
- **Accessibility**: VoiceOver and TalkBack read native UI's elements and
  act on them.
- **Android's back** pops the window pushed last, goes back in a page,
  closes what Escape closes in native UI, goes back in a `ui.Router`, or
  else leaves the app running in the background. The system knows
  beforehand, for its predictive back: a `ui.Router`'s page follows the
  gesture, and without anything to go back to the system shows the app
  leaving.
- **Lifecycle**: the app never quits itself. Going to the background is a
  power event `"suspend"` (`Power.OnSuspend`), and coming back `"resume"`
  and `App.OnActivate`. Frames stop in between. The system may end an app in
  the background: `App.OnSaveState` keeps what it needs to come back, which
  the next launch finds in `App.RestoredState`.
- **Memory**: `App.OnMemoryWarning` is called as the system runs short. Native
  UI lets go of its renderer's textures and its caches of text then, and the
  next frame makes them again.
- **Deep links** (`urlSchemes`) open the app with `App.OnOpenURL`.
- **Dialogs**: message boxes are the system's alerts; `Dialog.Open` shows
  its document picker and gives copies of the files chosen, in the app's
  temporary files. `Dialog.Save` gives a temporary file to write;
  `Dialog.SaveCopy` has the user choose where a file the app made goes.
- **Files**: `WatchFiles` watches the app's directories as on the
  desktop.
- **Secrets** go to the Keychain on iOS, and on Android to the app's
  storage, encrypted with a key of Android's keystore.
- **Notifications** ask for the user's permission the first time.
- What phones lack does nothing: menu bars, trays, the Dock, global
  shortcuts, the updater (stores update apps).

Android's web view loads no custom schemes: the app's frontend
(`mygo://localhost`) and its other schemes are served at
`https://<scheme>.localhost`, which pages never notice but in
`location.href`. Requests of `fetch` and `XMLHttpRequest` to the app's
schemes keep their bodies; a form posted to one arrives without its body
on Android, which does not give it, so post forms with `fetch` or through
bound methods.
