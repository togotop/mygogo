# 移动端（iOS / Android）架构说明

本文档说明 `daodao-mygo-mobile` 这一 mygo 分支**如何在不改动原版桌面架构的前提下，叠加 iOS / Android 支持**。

核心结论：移动端支持 = **复用原版 `platform.Backend` 抽象** + **新增两个 cgo 后端实现** + **一组统一的公共移动 API** + **两个原生宿主（UIKit / JNI）** + **build tag 分发**，所有未实现的能力统一回落到 `unsupported.Backend` 返回 `ErrUnsupported`。

---

## 1. 抽象层：复用原版 `platform.Backend`

原版 mygo 桌面端（macOS / Linux / Windows / WASM）已把平台能力抽象成 `internal/platform` 的 `Backend` 接口（`internal/platform/platform.go`）。移动端直接复用这一层——`Mobile()`、`NewWindow()`、`App()`、`Clipboard()`、`Theme()`、`Shell()`、`NotificationsSupported()` 等都是接口方法。

iOS / Android 的 backend 都通过**内嵌 `unsupported.Backend`**（`internal/unsupported`）拿到默认实现，只覆盖自己真正支持的那些方法。这是整个扩展策略的关键：未实现的能力不必写空实现，天然返回 `ErrUnsupported`。

## 2. 编译分发：`newBackend()` 按 build tag 选实现

顶层一组 `backend_*.go` 用 build tag 决定用哪个 backend（见 `mygo.go` 的 `backend()`）：

| 文件 | build tag | 后端 |
| --- | --- | --- |
| `backend_ios.go` | `ios && cgo` | `internal/ios` |
| `backend_android.go` | `android && cgo` | `internal/android/backend` |
| `backend_darwin.go` | `darwin && !ios` | `internal/darwin` |
| `backend_linux.go` | `linux && !android && (amd64||arm64)` | `internal/linux` |
| `backend_windows.go` | `windows && (amd64||arm64)` | `internal/windows` |
| `backend_js.go` | `js` | `internal/web` |
| `backend_other.go` | 移动端但 `!cgo` | `internal/unsupported` |

注意 `backend_other.go` 的 tag：`(android && !cgo) || (ios && !cgo) || (…)`。原因见 `backend_android.go` 的注释——`internal/android/backend` 依赖 cgo（EGL/JNI），若 `CGO_ENABLED=0` 时仍选中它会因“无可编译文件”而失败；关掉 cgo 时移动端应回落到 `unsupported`。

## 3. iOS 后端：cgo + UIKit，`ApplicationHost` 模式

目录 `internal/ios/`：

- `backend.go` 中的 `Backend` 内嵌 `unsupported.Backend`，只重写 `Mobile` / `Clipboard` / `Theme` / `App` / `Shell` 以及 `NewWindow`。
- iOS **只支持 `Surface`（原生 UI 窗口），不支持 webview 窗口**：`NewWindow` 要求 `o.Surface`，否则返回 `ErrUnsupported`；且目前仅支持单个 content 窗口。
- 它实现 `ApplicationHost`（`SystemManagedLifetime()=true`）：UIKit 拥有 OS 主线程，**Go 的 `main` 跑在后台线程**，UI 工作通过 cgo 调到 Objective-C。
- 桥接方式为 cgo 双向调用：
  - Go → ObjC：`#cgo LDFLAGS: -framework UIKit ...` + `native.h`/`native.m` 里的 `C.mygo_ios_*`（如 `mygo_ios_create`、`mygo_ios_dispatch`、`mygo_ios_haptic`）。
  - ObjC → Go：`//export goIOS*` 函数接收原生回调——`goIOSTouch`（触摸）、`goIOSLifecycle`（生命周期）、`goIOSNotification` / `goIOSPush`（通知 / APNs）、`goIOSText`（键盘）、`goIOSGesture`、`goIOSAccess`（辅助功能）等，转换后喂给 `WindowHandler` / `AppHandler` / `MobileHandler`。
- `mobile.go`（`internal/ios`）实现 `platform.Mobile`：`Haptic`、`SetBadge`、`SetStatusBar`、`RegisterPush`、`ScanCode`、`Device`、`NotificationsSupported` 等，均经 cgo 落到 UIKit。

## 4. Android 后端：c-shared `.so` + JNI + Gradle 宿主

分三层：

1. **`android/`**（仓库根目录）是完整的 Android Studio 工程（`app/`、`jni/`、`build.gradle`、`gradle.properties`、`gradlew` 等），即 Java/Kotlin 宿主。它用 `System.loadLibrary("mygo")` 加载 Go 编译出的 `libmygo.so`，提供 `Activity` / `MygoPlatform` 等。
2. **`internal/android/main.go`** 是 `c-shared` 的 `main` 包。buildmode=c-shared 必须有且仅有一个 main 包，且 main 包不能被 `import`；因此真正的 JNI `//export` 函数放在 **`android/jni`** 里，由这里 blank import（`_ "github.com/egoist/mygo/android/jni"`）。该 `main()` 为空——c-shared 模式加载库时不调用 `main`，一切由 Java 侧的 `//export` 函数驱动。
3. **`internal/android/backend/`** 才是真正的 backend（EGL / JNI / surface / frame / input / textinput），同样内嵌 `unsupported.Backend`，覆盖移动能力：`Haptic`（当前返回 `ErrUnsupported`）、`ScanCode`（调系统相机/相册扫码，经 Java 侧 `MygoPlatform.scanImage` 解码）、`DismissKeyboard` 等。

Android 比 iOS 特殊：`run_gate_android.go` 里 `runFromAnyGoroutine()=true`——因为 Java 拥有进程，Go 没有“主线程”概念，JNI 入口必须返回；`App.Run` 因此允许在任意 goroutine 调用，所有 UI 操作最终落到 backend 的 pump 上（见 `internal/android/backend` 的 `IsMainThread`）。

## 5. 公共移动端 API（顶层 `mygo` 包）

在原有 `Application` 上新增的跨平台 API，内部统一用 `onMain(...)` / `postMain(...)` 切回主线程，再调用 `backend().Mobile()` 或对应 platform 方法；不支持的平台返回 `ErrUnsupported`：

- **`mobile.go`**：
  - `Haptics.Play(kind)` —— 触觉反馈（iOS 与 Android）。
  - `(*Application).SetStatusBar(style, hidden)` —— 状态栏样式 / 隐藏（iOS 与 Android）。
  - `(*Application).OnBack(fn)` —— 系统返回手势（Android）。`fn` 在投递手势的线程上同步调用，只读状态并返回"是否消费"；要改页面的工作走 `RunOnMain`。
  - `(*Application).SetNotificationBadge(count)` —— iOS 应用角标。
  - `(*Application).RegisterPushNotifications()` / `OnPushToken` / `OnPushRegistrationError` —— APNs 注册（iOS）。
  - `(*Application).OnNotification` / `SetNotificationPresentationHandler` —— 前台通知投递与展示策略（iOS）。事件经 `appHandler.NotificationReceived` 转发到 `mobileEvents`。
- **`mobile_dialog.go`**：
  - `Dialog.Export` —— 系统导出选择器（当前 iOS 支持）。
  - `Dialog.Photos` —— 系统照片选择器（iOS PHPicker，免相册全量授权；返回私有缓存副本路径）。
- **`paths.go`**：Android 的 `PathUserData` / `PathCache` / `PathLogs` 取自宿主在启动时报告的 `getFilesDir()` / `getCacheDir()`（`paths_android.go`），因为 Android 没有 home 目录，`os.UserHomeDir` 会给出不可写的 `/sdcard`。
- **`app_group_ios.go`**：`hostAppGroupDirectory` —— iOS App Group 共享目录（仅 `ios && cgo`）。
- **`background_refresh_ios.go` / `background_refresh_other.go`**：iOS 后台刷新（其余平台空实现）。

## 5.1 Android 宿主契约（Native）

宿主通过 `org.mygo.Native` 的静态 native 方法把系统事件交给 Go，符号是 `Java_org_mygo_Native_*`：

| 方法 | 时机 |
| --- | --- |
| `nativeOnSurfaceCreated/Changed/Destroyed` | Surface 生命周期 |
| `nativeOnTouch` / `nativeOnKey` / `nativeOnSizeChanged` / `nativeOnSafeArea` | 输入与尺寸 |
| `nativeOnPaths(data, cache)` | 启动时报告应用私有目录 |
| `nativeOnLifecycle(state)` | 0 resumed / 1 paused / 2 started / 3 stopped |
| `nativeBack()` | 返回手势，返回是否已消费 |

生命周期事件常在应用 ready 之前到达（`onStart`/`onResume` 早于第一次 surface 回调里的 `run()`）。后端记住最后一个状态，在 `markReady` 里补发，应用才能以真实状态开始。

## 6. 线程模型适配

- `mygo.go` 的 `init()`：`runtime.LockOSThread()` 锁定主线程，但**仅当 `runtime.GOOS != "ios"`**——因为 iOS 主线程由 UIKit 持有，Go 在后台线程运行。
- `Run` 行为：
  - 桌面端：`Run()` 必须作为 `main` 的最后一行（事件循环拥有进程主线程）。
  - 移动端：`runFromAnyGoroutine()` 决定——桌面为 `false`，Android 为 `true`。iOS 的 `Run()` 直接返回错误“iOS uses the UIKit application host”，实际走 `RunApplication(start, finish)`（由 `ApplicationHost` 定义），UIKit 在后台线程托管生命周期。

## 7. 能力支持现状（速查）

| 能力 | iOS | Android | 桌面 |
| --- | --- | --- | --- |
| 原生 UI 窗口（`Surface`） | ✅（唯一窗口类型） | ✅ | ✅（+ webview） |
| Webview 窗口 | ❌ `ErrUnsupported` | ❌ | ✅ |
| Haptics | ✅ | ✅ | ❌ |
| 状态栏 | ✅ | ✅ | ❌ |
| 应用角标 | ✅ | ❌ | ❌ |
| 返回手势 | ❌ | ✅ | ❌ |
| 前后台生命周期 | ✅ | ✅ | ✅ |
| 网络状态（在线 / 计费） | ✅ | ✅（每 5 秒轮询） | ✅ |
| APNs / FCM 推送 | ✅ | ✅（已接线，需应用自备 Firebase） | 部分（桌面通知） |
| 系统照片选择器 | ✅ | ✅ | ❌ |
| 系统扫码（相机/相册） | ✅ | ✅（系统选择器） | ❌ |

> 标记为 ❌ 的能力并非“未开发”，而是统一由内嵌的 `unsupported.Backend` 返回 `ErrUnsupported`。上层（`ui` 包等）对不支持项做静默降级（例如 Android 的 Haptic 被忽略）。

---

## 一句话扩展方法

> 在原版 mygo 上加 iOS/Android 支持 = **给 `platform.Backend` 加两个 cgo 实现（iOS 用 UIKit、Android 用 EGL+JNI），用 build tag 在 `newBackend()` 分发，把原生宿主（`.m`/`.h` 与 Gradle 工程）接进来，再在顶层封装一组统一的 Mobile API，所有未实现的能力默认回落到 `unsupported.Backend` 返回 `ErrUnsupported`。**

## 相关文件索引

- 抽象接口：`internal/platform/platform.go`（`Backend` / `Mobile` / `ApplicationHost` / `WindowHandler` 等）
- 兜底实现：`internal/unsupported`
- 分发：`backend_*.go`、`mygo.go`（`backend()`、`init()`）、`run_gate_*.go`
- iOS：`internal/ios/{backend,mobile,window,app_group,network,permissions,secrets,speech}.go` 及 `*.m`/`*.h`
- Android：`android/`（Gradle 宿主）、`internal/android/main.go`、`internal/android/backend/*`、`android/jni/*`（JNI 导出）
- 公共 API：`mobile.go`、`mobile_dialog.go`、`app_group_ios.go`、`background_refresh_*.go`
