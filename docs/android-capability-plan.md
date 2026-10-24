# Android 移动能力补齐实施计划

> 仓库：`daodao-mygo-mobile`（`github.com/egoist/mygo` 的移动分支）
> 状态：计划稿，按阶段逐步实现
> 最后核实：2026-10-21（基于当前代码，非文档注释）
> 修订：2026-10-21 评审 v2 —— 修正 kind 数量与 Export 多文件契约，补异步回传决策、request code 协调、SafeArea 联动、触觉映射等 7 处；追加附录 B（Android 编译环境安装）
> 更正：Debian 13（trixie）仓库无 openjdk-17，最低 21 —— JDK 改用 `openjdk-21-jdk`。
> 补充：编 `.so` 需预置 Android HarfBuzz/FreeType 静态库（见 B.5.1）—— 实测缺失会 `exit 2`。
> 实测：2026-10-22 全链路通过（sdkmanager → .so 16KB 对齐 → Gradle `assembleDebug`，APK 含 `libmygo.so`；代理环境的 Gradle 额外参数见 B.6.1）。
> 补充：B.8 装机与首次运行验证（真机 usbipd-win / WSL 模拟器 / 失败模式对照）。
> 进度：2026-10-22 已合并上游 0.6.2（2fb2cd3）；Phase 0/1/2(StatusBar)/3/4(接线) 已在模拟器验证；Badge 定为框架层不做，真 token 待 GMS/包名。

## 0. 目标与范围

**目标**：把 `platform.Mobile` / `platform.Dialogs` 中 Android 仍返回 `ErrUnsupported` 或空实现的能力，用 Kotlin 补到可用。

**范围**：Haptic、StatusBar、Badge、Photos、Export、Push。
**非目标**：不改 EGL/GPU 渲染；不动 iOS；不恢复 webview（移动端本就不支持）；不重构桥接架构。

## 1. 现状（已核实）

### 1.1 桥接机制（现成，照抄即可）

- **Go → Java**：`backend.PlatformBridge` 接口（`internal/android/backend/platform.go:70`）由 `androidJNI`（`android/jni/jni_platform.go:610`）实现；`backend.SetPlatformBridge(androidJNI{})` 在 `jni_platform.go:607` 安装。
- 方法名 + 签名集中在 `mygo_platform_attach` 的 `lookups[]`（约 `jni_platform.go:433`），`GetMethodID` 结果存入 `mygo_mid_*` 静态槽；调用用 `CallIntMethod` / `CallVoidMethod`（**同步**）。
- 跨 JNI 文本一律 `ByteArray`（UTF-8）收发，**不要用 String**：JNI 的 modified UTF-8 不能带 NUL，且 BMP 外字符（emoji、部分 CJK 扩展）会错。
- 每次调用后必须清理 Java 异常（`mygo_platform_exception`）；残留异常会让后续调用静默返回零值。
- **Java → Go**：Kotlin `object MygoPlatform` 的 `external fun` → Go 的 `//export Java_org_mygo_MygoPlatform_<name>`（`jni_platform.go:1021+`）。**类名必须逐字是 `org.mygo.MygoPlatform`**，且成员挂在类本身而非 companion。
- **线程**：Go 侧调用来自 pump 线程。`ClipboardManager` / `NotificationManager` / `startActivity` 从任意线程合法；只有 **View 操作**（`announceForAccessibility` / `sendAccessibilityEvent` / `getLocationOnScreen`）必须 `onUi { }`。
- **异步回传（扫码先例，拉模式）**：Kotlin `deliverScanResult`（`MygoPlatform.kt:1000`）只把结果写进 `pendingResult`，不调 Go；Go 常驻 goroutine `collectScanResults()`（`jni_platform.go:858`）轮询 `takeScanResult()`，拿到才调 `backend.ScanResult` 分发。Phase 3 的回传方式决策见该节。

### 1.2 当前能力状态

| 能力 | 顶层 API | 当前 Android 行为 | 结论 |
| --- | --- | --- | --- |
| Haptic | `Haptics.Play` | `ErrUnsupported` | 待实现 |
| StatusBar | `App.SetStatusBar` | `ErrUnsupported` | 待实现 |
| Badge | `App.SetNotificationBadge` | 空实现（**且挂起**，见 1.3） | 待修+实现 |
| Photos | `Dialog.Photos` → `Dialogs()` | unsupported | 待实现 |
| Export | `Dialog.Export` → `Dialogs()` | unsupported | 待实现 |
| Push | `App.RegisterPushNotifications` | `ErrUnsupported` | 待实现 |
| ScanCode | `Mobile.ScanCode` | ✅ 已实现 | 参照模板 |
| Clipboard | `Clipboard.*` | ✅ 已实现 | — |
| Notifications | `Notification.*` | ✅ 已实现 | — |
| OpenExternal | `Shell.OpenExternal` | ✅ 已实现 | — |

### 1.3 已发现的缺陷（Phase 0 必须先修）

`androidMobile.SetBadge(int, func(error))` 是空实现，**从不调用 `done`**。而顶层 `SetNotificationBadge` 走 `await(ch)`（`loop.go:119`，**无超时**：主线程循环 `backend().Step()`，其他 goroutine 直接 `<-ch`）。
⇒ **在 Android 上调 `SetNotificationBadge` 会永久阻塞。** 先修，再谈补齐。

### 1.4 Activity 基类与工程现状

- `MygoActivity : Activity()`（`android/app/src/main/java/org/mygo/MygoActivity.kt:21`）——纯 `android.app.Activity`，**不是 ComponentActivity**，因此**不能用 AndroidX `registerForActivityResult`**。异步选图/导出走 classic `startActivityForResult` + `onActivityResult`，**不要为此迁移基类**。
- Manifest 已有 `POST_NOTIFICATIONS`、`INTERNET`；**缺 `VIBRATE`**（Haptic 需要，normal 权限，安装即授予）。
- `minSdk 29`、`targetSdk 36`、`compileSdk 36`（`android/app/build.gradle`）。

## 2. 设计原则

1. cgo 消不掉（`.so`/JNI 边界 + EGL），但**每个能力只加一段薄 JNI 转发**，不碰 EGL。
2. 有限枚举（haptic / statusBar）用 `Int` 传，避免 `byte[]` 编解码；只有自由文本才用 `byte[]`。
3. 同步能力 → `PlatformBridge` 同步方法；异步（选图/导出）→ **token + 结果回传**，优先照抄扫码的**拉模式**（`startScan` / `takeScanResult` + collect goroutine），见 Phase 3 决策 1。
4. 所有 Java 调用先检查 `mygo_platform_ready` / 方法槽，失败返回 `ErrPlatformBridge`，不得崩溃。
5. 顶层 `mygo` 公共 API **无需改动**（已 route 到 `Mobile()` / `Dialogs()`）。

## 3. 改动模板（每个能力，以 Haptic 为例）

一次完整改动涉及 **6 个位置**：

1. **`internal/android/backend/platform.go`** — `PlatformBridge` 接口加方法：
   ```go
   Haptic(kind string) error
   ```
2. **`android/jni/jni_platform.go`**：
   - 加 `static jmethodID mygo_mid_haptic = NULL;`
   - `lookups[]` 加一项：`{"haptic", "(I)I", &mygo_mid_haptic}`
   - 加 C helper `static jint mygo_platform_haptic(JNIEnv *env, jint kind)`（取 env → 查槽 → `CallIntMethod` → `mygo_platform_exception`）
   - `androidJNI` 加 Go 方法 `func (androidJNI) Haptic(kind string) error`
3. **`internal/android/backend/mobile.go`** — `androidMobile.Haptic` 改为调 `platformBridge().Haptic(kind)`。
4. **`android/app/src/main/java/org/mygo/MygoPlatform.kt`** — `fun haptic(kind: Int): Int`。
5. **`internal/android/backend/platform_test.go`** — `fakeBridge` 加同名方法（**否则编译失败**）。
6. **`android/app/src/main/AndroidManifest.xml`** — 按需加权限（Haptic 加 `VIBRATE`）。

> ⚠️ `PlatformBridge` 是 Go 接口，加方法会同时打破 `androidJNI` 与 `fakeBridge` 两处实现；漏改任一处 `go build`/`go test` 直接挂。
> 最快的反馈回路：`CGO_ENABLED=0 go test ./internal/android/backend`（`platform.go` 无 build tag，可在桌面跑）。

> ⚠️ 上述 6 处是**同步能力**的模板。异步能力（Photos/Export/Push）另有触点：`MygoActivity.kt`（`onActivityResult` 分发 + request code）、`android/jni/jni_platform.go` 的结果分发（collect goroutine 或新 `//export`）、Go 侧 handler 路由（`platform.MobileHandler` 类型断言，参照 iOS backend 写法）。详见 Phase 3/4。

## 4. 分阶段计划

### Phase 0 — 前置修复与地基（无新功能）— ✅ 2026-10-22 完成

> 实测：`smoke: startup SetNotificationBadge(3) -> not supported on this platform`（立即返回，不再挂起）。
> 顺带修复：`internal/gpu/gl` 在 Android 下缺 `hasContext()`（`android` 同时满足 `linux` tag，`present.go` 被编译），已补实现。
- [ ] 修 `androidMobile.SetBadge`：调用 `done(platform.ErrUnsupported)`，消除挂起。
- [ ] Manifest 加 `<uses-permission android:name="android.permission.VIBRATE" />`。
- [ ] 确认 `MygoActivity` 保持 `Activity()`，异步走 `startActivityForResult`。
- 验收：Android 上调 `SetNotificationBadge` 立即返回 `ErrUnsupported`，不再卡死。

### Phase 1 — Haptic（试点，验证整条链路）— ✅ 2026-10-22 完成

> 实测：`smoke: Haptics.Play(medium) -> <nil>`（接口→JNI 表→Kotlin Vibrator 全链路）。
- 接口 `Haptic(kind string) error`；`androidMobile.Haptic` 转发到 bridge。
- **映射决策（先做，映射表写进代码注释）**：9 种 kind 中 selection/light/medium/heavy/soft/rigid 可映射 `VibrationEffect` 预定义效果/组合；**success/warning/error 是 iOS `UINotificationFeedbackGenerator` 语义，Android 无原生对应**，需自定组合（composition 或 pattern）。
- Kotlin：`Vibrator` + `VibrationEffect`（API 26+）/ `vibrate(long)`（24–25）；先 `hasVibrator()` 判空。
- 验收：真机 9 种 kind 可感知差异（后三种按自定映射）；无马达设备返回 `ErrUnsupported` 而非崩溃。
- 产出：跑通"接口 → lookups 表 → JNI helper → Kotlin"模板，供后续能力复用。

### Phase 2 — StatusBar + Badge（同步，复用 Phase 1 模板）— StatusBar ✅ / Badge ❌（框架层）

> **StatusBar ✅ 2026-10-22**：`smoke: SetStatusBar(dark) -> <nil>`。
> **Badge ❌ 框架层不做（2026-10-22 决定）**：Android 没有标准角标 API——通知点是系统行为（用户可关，只表示有未读），数字角标是 launcher 私有实现；`ShortcutBadger` 是事实标准但 2019 年后基本停更，覆盖不可靠。强行实现会给出“返回成功但图标没数字”的假象，比 `ErrUnsupported` 更糟。
> 框架保持 `ErrUnsupported`（不再挂起）；Android 请用通知（系统圆点），需要数字角标的 app 自行用 launcher 私有方案（best-effort）。

- **StatusBar**：`SetStatusBar(style string, hidden bool) error`；Kotlin 用 `WindowInsetsController`（API 30+）与 `systemUiVisibility`（24–29）分支；`style` → `isAppearanceLightStatusBars`。
- ⚠️ **StatusBar 隐藏 ↔ SafeArea 联动**：SafeArea 由 Java 经 `Native.onSafeArea` 推（`internal/android/backend/app.go:408` → `win.h.SafeAreaChanged()`）。隐藏状态栏会改 insets，须验证 Java 侧 insets 监听会把变化推下来，否则 Go UI 留旧的安全区内边距。
- **Badge**：不做（见上方决定）。`SetBadge` 保持 `ErrUnsupported`，调用方立即得到结果。
- 验收：状态栏字色/隐藏生效；Badge 在 Android 上返回 `ErrUnsupported` 且不挂起。

### Phase 3 — Photos + Export（异步，走 `platform.Dialogs`）— ✅ 2026-10-22 完成

> 实现按两条决策：**拉模式**（Kotlin `pendingMedia` 队列 + `takeMediaResult`，Go `collectMediaResults` 轮询）与**单文件 Export**（多文件返回 `ErrUnsupported`）。
> request code：`PHOTOS_REQUEST=0x5CAA`、`EXPORT_REQUEST=0x5CAB`（与扫码 `0x5CA9` 避让）。
> 实测：`Dialog.Photos(multiple) -> 1 paths`（成功后返回缓存副本）、`-> 0 paths, err=<nil>`（取消）；`Dialog.Export(one) -> true`（文件已写入 Download）与 `false, err=<nil>`（取消）；`Dialog.Export(two) -> not supported`。
> 注意：顶层是 `Dialog.Photos` / `Dialog.Export` → `backend().Dialogs()`，**不是 `Mobile()`**。Android 侧需 override `Dialogs`。

**两个设计决策，实现前定死：**
1. **回传方式：拉 vs 推**
   - 拉模式（扫码先例）：Kotlin 把结果入 pending 队列，Go 的 collect goroutine 轮询分发。与现状一致、可泛化 `collectScanResults()`；代价是结果有轮询周期延迟。
   - 推模式：新增 `//export` 回调（`Native.java` 注释明说允许新增 native 方法），更及时；但 Java UI 线程进 Go 后**必须 `b.Post` 回 pump** 再回调（参照 `OnNotificationClicked`，`platform_android.go:56`）。
   - **推荐：先拉模式**，把 `collectScanResults` 泛化为通用 pending-result 分发；跑稳后再评估推。
2. **Export 多文件**：平台契约是 `ExportDialogOptions.Files []string`（多文件）。SAF `ACTION_CREATE_DOCUMENT` 一次只建一个文档。三选一：
   a) 逐文件顺序弹选择器（契约完整，体验差）；
   b) **首期单文件，多文件返回 `ErrUnsupported` 并在顶层 `mobile_dialog.go` 文档注明**（推荐起步）；
   c) `ACTION_OPEN_DOCUMENT_TREE` 目录导出（语义变化，需上游认可）。

- 新增 `androidDialogs`，内嵌 unsupported 的 Dialogs，覆盖 `ShowPhotoDialog` / `ShowExportDialog`。
- token 机制：Go 生成 token → bridge 调 Kotlin 启动选择器 → Kotlin `onActivityResult` 取 Uri → 复制到 `cacheDir` → 结果入 pending 队列 → Go collect 分发。
- **request code 协调（必须）**：`MygoActivity.onActivityResult`（`MygoActivity.kt:104`）目前只转发 `MygoPlatform.onScanResult`，按 `SCAN_REQUEST = 0x5CA9`（`MygoPlatform.kt:957`）过滤。新增 `PHOTOS_REQUEST` / `EXPORT_REQUEST`，在同一分发处按 code 路由，**避免与扫码冲突**。
- Kotlin：
  - 选图：API 33+ 用 `MediaStore.ACTION_PICK_IMAGES`（扫码已用，有先例）；24–32 回退 `ACTION_GET_CONTENT`（`image/*`）。多选加 `EXTRA_ALLOW_MULTIPLE`。
  - 导出：`Intent.ACTION_CREATE_DOCUMENT`（SAF），用 `ContentResolver.openOutputStream` 写入。
  - 多路径返回编码：沿用 `clipboardMimeTypes` 的**换行拼接 byte[]** 先例，避免 `[[B` 对象数组 JNI 胶水。
- 本阶段触点（超出同步模板 6 处）：`MygoActivity.kt`（onActivityResult 分发）、`MygoPlatform.kt`（结果处理 + request code 常量）、`android/jni/jni_platform.go`（bridge 方法 + collect 分发）、Go 侧 `androidDialogs` 覆盖层。
- 验收：选图返回缓存副本路径；导出落盘成功；取消返回 `nil, nil` / `false, nil`，与 iOS 语义一致。

### Phase 4 — Push（FCM，最重，需工程改动）— 🔶 2026-10-22 接线完成

> 实现：**不接 google-services Gradle 插件**（AGP 9 newDsl 兼容性），改为把 JSON 放 `src/main/assets/`、`ensureFirebase` 用 `FirebaseOptions` 手动初始化；token 走 Phase 3 的拉队列（kind 2 = token，kind 3 = error），Go `PushRegistered` 交给 `MobileHandler`。
> 另加 **10s 超时兜底**：无 GMS 设备上 Firebase 的 token task 不返回，超时后报 `token request timed out (is Google Play services available?)`，避免应用永远等。
> 实测（x86_64 模拟器，无 GMS）：`RegisterPush -> <nil>`，随后 `push error: token request timed out ...`。
> **待办**：需要 GMS 真机/镜像才能拿到真 token；且 `google-services.json` 的包名是 `top.togoto.mygo`，宿主 `applicationId` 是 `org.mygo.host` —— 真机前要么在 Firebase 项目加 `org.mygo.host`，要么把 `applicationId` 改成 `top.togoto.mygo`。
- `RegisterPush() error` + `OnPushToken` / `OnPushRegistrationError` 回传。
- Kotlin/工程：加 Firebase Messaging 依赖 + `google-services` 插件 + `google-services.json`；实现 `FirebaseMessagingService.onNewToken/onMessageReceived`；回传走 Phase 3 定下的拉/推机制。
- **Go 侧路由**：backend 里类型断言 `b.h.(platform.MobileHandler)` 调 `PushRegistered(token, err)`（照抄 iOS backend 写法），token 事件才能到达顶层 `OnPushToken` / `OnPushRegistrationError`。
- 验收：真机拿到 token；前台/后台通知点击能回到 `OnNotification`。
- 风险：依赖外部配置文件，非纯代码，需构建侧配合。

## 5. 能力明细对照表

| 能力 | Go 接口（建议） | Kotlin fun | JNI 签名 | 类型 | 权限/依赖 |
| --- | --- | --- | --- | --- | --- |
| Haptic | `Haptic(kind string) error` | `haptic(kind: Int): Int` | `(I)I` | 同步 | VIBRATE |
| StatusBar | `SetStatusBar(style string, hidden bool) error` | `setStatusBar(style: Int, hidden: Boolean): Boolean` | `(IZ)Z` | 同步 | 无 |
| Badge | `SetBadge(count int) error` | `setBadge(count: Int): Int` | `(I)I` | 同步 | POST_NOTIFICATIONS(13+) |
| Photos | `PickPhotos(token uint64, multiple bool) error` | `pickPhotos(token: Long, multiple: Boolean)` | `(JZ)V` | 异步 | 无（Photo Picker） |
| Export | `Export(token uint64, files []string, title string) error` | `export(token: Long, files: ByteArray, title: ByteArray)`（files 换行拼接） | `(J[B[B)V` | 异步 | 无（SAF） |
| Push | `RegisterPush() error` | `registerPush(): Int` | `()I` | 异步 | FCM 依赖 |

> 异步结果回传默认走**拉模式**（Kotlin pending 队列 + Go collect 轮询，与扫码一致）。若改推模式，则新增 `external fun`：`nativePhotoResult(J, [B?)`、`nativeExportResult(J, Z, [B?)`、`nativePushToken([B?, [B?)`，对应 Go `//export Java_org_mygo_MygoPlatform_native*`，且回调必须 `b.Post` 回 pump（见 Phase 3 决策 1）。

> **Badge 已决定不在框架层实现**（见 Phase 2）：Android 无标准角标 API，保持 `ErrUnsupported`；应用需要数字角标时自行处理（best-effort）。

## 6. 测试策略

- **快速回路**：`CGO_ENABLED=0 go test ./internal/android/backend` —— `platform.go` 无 build tag，desktop 可编译；新方法在 `fakeBridge` 里断言。
- **JNI/真机**：每阶段跑对应验收清单；重点验证异常路径（无权限、取消、无硬件）。
- **回归**：确认 ScanCode / Clipboard / Notifications / OpenExternal 未被破坏。
- **静态检查**：`go vet`、`gofmt`；Kotlin 侧 `./gradlew assembleDebug`。

## 7. 风险与坑（务必遵守）

1. **挂起**：`SetNotificationBadge` 现会永久阻塞（Phase 0 已列）。
2. **JNI 异常不清理** → 后续调用静默返回零值；每个 helper 必须 `mygo_platform_exception`。
3. **字符串**：一律 `byte[]` UTF-8，禁止 `String`。
4. **`MygoActivity` 是 plain Activity** → 不能用 AndroidX `registerForActivityResult`。
5. **minSdk 29** → Photo Picker 需回退（API 33+）；`ACTION_CREATE_DOCUMENT` 19+ 可用。
6. **接口加方法** → 必改 `fakeBridge`，否则编译失败。
7. **Badge** 框架层不做（无标准 API、跨启动器不一致）；应用若需要数字角标，自行用 launcher 私有方案并接受 best-effort。
8. **targetSdk 36**：注意 Android 16 的行为差异（见 `build.gradle` 注释）。
9. **request code 冲突**：新选择器必须用独立 request code，与 `SCAN_REQUEST(0x5CA9)` 避让，并在 `MygoActivity.onActivityResult` 统一分发。
10. **Export 多文件契约**：平台契约是 `Files []string`；若首期只做单文件，须在顶层文档注明，别静默吞掉多文件调用。

## 8. 验收总清单

- [x] Phase 0：`SetNotificationBadge` 不再挂起；`VIBRATE` 已声明。
- [x] Phase 1：Haptic 链路已验证（medium）；其余 kind 共用同一映射。
- [x] Phase 2：StatusBar 样式/隐藏生效；Badge 定为框架层不做（`ErrUnsupported`，不挂起）。
- [x] Phase 3：拉模式与单文件 Export 决策落地；request code 与扫码避让；Photos 成功/取消、Export 成功/取消/多文件均已验。
- [x] Phase 4（接线）：`RegisterPush` 返回并回传错误；真 token 待 GMS 设备与包名对齐。
- [ ] 全阶段：`go test ./internal/android/backend` 通过；`assembleDebug` 通过；旧能力回归通过。

---

## 附录 A：最小起步（Phase 0 + Phase 1 的 diff 范围）

1. `internal/android/backend/mobile.go`：`SetBadge` 补 `done(...)`；`Haptic` 改调 bridge。
2. `internal/android/backend/platform.go`：`PlatformBridge` 加 `Haptic`。
3. `android/jni/jni_platform.go`：加 jmethodID 槽 + lookups 项 + C helper + `androidJNI.Haptic`。
4. `android/app/src/main/java/org/mygo/MygoPlatform.kt`：加 `haptic`。
5. `internal/android/backend/platform_test.go`：`fakeBridge.Haptic`。
6. `android/app/src/main/AndroidManifest.xml`：加 `VIBRATE`。

跑通后，Phase 2 照此模板复制；Phase 3/4 有异步额外触点（见第 3 节末尾的注与 Phase 3/4 说明）。

---

## 附录 B：Android 编译环境安装

> 依据：`scripts/android-doctor.sh` / `scripts/android-build.sh` / `android/README.md` / `android/app/build.gradle`（2026-10-21 核实）。

### B.1 必装（缺一不可）

| 组件 | 版本 | 用途 | 安装方式 |
| --- | --- | --- | --- |
| Go | **1.27.1** | 交叉编译 `libmygo.so` | `mise install`（仓库 `mise.toml` 已锁版本） |
| JDK | **21**（Debian 13 仓库最低；也可装 `openjdk-25-jdk`） | AGP 9.0 最低要求 17，但 Debian 13 不提供 17；21 可跑（工程字节码 target 仍为 17，见 `app/build.gradle:103`） | `apt install openjdk-21-jdk`（须设 `JAVA_HOME`） |
| Android SDK Platform | `android-36` | compileSdk 36 | `sdkmanager "platforms;android-36"` |
| Build-Tools | `36.0.0`（或 `36.1.0`） | AGP 9.0 默认组合 | `sdkmanager "build-tools;36.0.0"` |
| NDK | **`28.2.13676358`（r28）** | 提供 `aarch64-linux-android29-clang`；r28 默认 16KB 页对齐 | `sdkmanager "ndk;28.2.13676358"` |
| platform-tools | latest | `adb` | `sdkmanager "platform-tools"` |
| cmdline-tools | `latest` | 提供 `sdkmanager` / `avdmanager`（先手动解压才能装上面这些） | 解压 Google `commandlinetools-linux-*.zip` |

### B.2 可选

- `emulator` + 系统镜像（无真机时）：`sdkmanager "emulator" "system-images;android-36;google_apis;arm64-v8a"`
- Android Studio（图形化管理 SDK/AVD，非必需）

### B.3 不用装

- **Gradle 9.1.0**：`./gradlew` 自动下载（有网）或复用 `~/.gradle` 缓存
- **AGP 9.0.1**：Gradle 自动拉依赖
- **Kotlin 插件**：AGP 9.0 内置 Kotlin（`build.gradle` 故意不声明 `kotlin-android`）
- **CMake / externalNativeBuild**：`.so` 由 Go 单独产出，工程内不配 NDK 编译

### B.4 环境变量

```bash
export ANDROID_HOME="$HOME/Android/Sdk"     # 脚本也读 ANDROID_SDK_ROOT
export ANDROID_SDK_ROOT="$ANDROID_HOME"
export JAVA_HOME=/usr/lib/jvm/java-21-openjdk-amd64
```

可选覆盖：`ANDROID_NDK_VERSION`（默认 `28.2.13676358`）、`MYGO_ANDROID_CC`、`MYGO_ANDROID_PKG`。

> 若 SDK 装在非默认位置（例如 `$HOME/android`），把 `ANDROID_HOME` 指过去即可——`android-doctor.sh` / `android-build.sh` 优先读环境变量，不会硬套 `$HOME/Android/Sdk`。

### B.5 Linux/WSL 装机流程

```bash
# 0. JDK（Debian 13 只有 21+，没有 17）
sudo apt install openjdk-21-jdk

# 1. 引导 sdkmanager（cmdline-tools 必须先手动放好）
mkdir -p "$HOME/Android/Sdk/cmdline-tools"
unzip commandlinetools-linux-*.zip -d "$HOME/Android/Sdk/cmdline-tools/latest"
export ANDROID_HOME="$HOME/Android/Sdk"; export JAVA_HOME=/usr/lib/jvm/java-21-openjdk-amd64

# 2. 接受许可 + 装包
yes | "$ANDROID_HOME/cmdline-tools/latest/bin/sdkmanager" --licenses
"$ANDROID_HOME/cmdline-tools/latest/bin/sdkmanager" \
  "platforms;android-36" "build-tools;36.0.0" "ndk;28.2.13676358" \
  "platform-tools" "cmdline-tools;latest"

# 3. Go 1.27.1
cd ~/gitrap/daodao-mygo-mobile && mise install
```

### B.5.1 HarfBuzz 静态库（编译 `.so` 前必须）

`scripts/android-build.sh` 在编 `.so` 前会检查 `third_party/hb-build/src/lib/android/lib/libharfbuzz.a`，缺失则直接 `exit 2`。二选一：

```bash
# 推荐：下载预编译（约 4MB）
mkdir -p third_party/hb-build/src/lib
curl -fsSL https://github.com/Heyana/mygo-upstream/releases/download/hb-android-arm64/hb-android-arm64.tar.gz \
  | tar xz -C third_party/hb-build/src/lib

# 或本机编（需 meson/ninja + FreeType/HarfBuzz 源码，较重）
bash third_party/hb-build/build.sh android
```

产物：`third_party/hb-build/src/lib/android/lib/{libharfbuzz.a,libfreetype.a}`（`src/` 已被 gitignore）。

### B.5.2 模拟器（x86_64）的 HarfBuzz

上游只提供 arm64 预编译包；x86_64 模拟器上跑含文本的 UI 需本地交叉编译一份（否则链接报 `libharfbuzz.a ... incompatible with elf_x86_64`）：

```bash
uv tool install meson ninja      # 或 apt install meson ninja-build
# 用 NDK 的 x86_64-linux-android29-clang 编 freetype/harfbuzz，
# 产物放 third_party/hb-build/src/lib/android-x86_64（与 arm64 的 android/ 分开）
```

构建 .so 时指向对应目录：

```bash
HB=third_party/hb-build/src/lib/android-x86_64
CGO_CFLAGS="-I$HB/include/harfbuzz -I$HB/include/freetype2" \
CGO_LDFLAGS="-L$HB/lib -lharfbuzz -lfreetype -lm -llog" \
GOOS=android GOARCH=amd64 CGO_ENABLED=1 CC=$NDK/toolchains/llvm/prebuilt/linux-x86_64/bin/x86_64-linux-android29-clang \
go build -buildmode=c-shared -o android/app/src/main/jniLibs/x86_64/libmygo.so ./examples/<app>
```

实测：meson 1.12.1 + ninja 1.13.2（uv 安装）可完成；APK 的 `abiFilters` 需含 `x86_64`。

### B.6 自检与构建

```bash
bash scripts/android-doctor.sh     # 逐项 ✅/🔴，缺什么给出补法
bash scripts/android-build.sh      # 编 .so → 校验 16KB 对齐 → 跑 Gradle
bash scripts/android-run.sh        # adb install + am start + logcat
```

### B.6.1 代理环境（Gradle 不读 HTTPS_PROXY）

Gradle wrapper / 守护进程是 JVM，**不读** `HTTPS_PROXY` / `http_proxy`，会报 `java.net.ConnectException: Connection timed out`。二选一：

```bash
# a) 给 Gradle 传 JVM 代理参数（依赖下载也走代理）
./android/gradlew -p android --no-daemon \
  -Dhttp.proxyHost=127.0.0.1 -Dhttp.proxyPort=20122 \
  -Dhttps.proxyHost=127.0.0.1 -Dhttps.proxyPort=20122 \
  :app:assembleDebug

# b) 用 curl（读 HTTPS_PROXY）手动下发行版到 wrapper 缓存
HASH=<wrapper 目录名>
D="$HOME/.gradle/wrapper/dists/gradle-9.1.0-all/$HASH"
curl -fL -o "$D/gradle-9.1.0-all.zip" https://services.gradle.org/distributions/gradle-9.1.0-all.zip
cd "$D" && unzip -q -o gradle-9.1.0-all.zip && touch gradle-9.1.0-all.zip.ok
```

实测：代理 `127.0.0.1:20122` 下用 a) 成功，产物 `android/app/build/outputs/apk/debug/app-debug.apk`（4.1MB，含 `lib/arm64-v8a/libmygo.so`）。

### B.7 两个坑

1. **只打 `arm64-v8a`**（`abiFilters 'arm64-v8a'`）。x86_64 模拟器加载 arm64 `.so` 会 `UnsatisfiedLinkError`：要么用 arm64 镜像（x86 主机上慢），要么改 `abiFilters` 并用 `GOARCH=amd64` 额外编一份。
2. **WSL 连真机**需 Windows 侧 `usbipd-win` 做 USB 转发；否则走模拟器。

### B.8 装机与首次运行验证

**前置**：`adb devices -l` 为空表示当前无设备。二选一。

#### A. 真机（WSL）—— Windows 侧 usbipd-win

```powershell
# Windows（管理员 PowerShell）
usbipd list
usbipd bind --busid <BUSID>
usbipd attach --wsl --busid <BUSID>
```

```bash
# WSL
adb devices -l      # 应能看到设备
```

手机侧：打开「开发者选项 → USB 调试」，首次连接在手机上点「允许」。

#### B. 模拟器（WSL 内，用 KVM）

```bash
sdkmanager "emulator" "system-images;android-36;google_apis;arm64-v8a"
avdmanager create avd -n mygo -k "system-images;android-36;google_apis;arm64-v8a" -d pixel_7
emulator -avd mygo -no-snapshot -gpu swiftshader_indirect
```

本机 `/dev/kvm` 存在，但当前用户不在 `kvm` 组（不可读写）：

```bash
sudo usermod -aG kvm "$USER"   # 之后需重新登录（或 newgrp kvm）
```

⚠️ APK 只含 `arm64-v8a`，**必须用 arm64 系统镜像**；x86_64 镜像会 `INSTALL_FAILED_NO_MATCHING_ABIS`。WSL 内跑模拟器还需 WSLg（GUI）；无 KVM 时极慢，不建议。

#### 装机与启动（两条路通用）

```bash
scripts/android-run.sh
# 等价于：
adb install -r android/app/build/outputs/apk/debug/app-debug.apk
adb shell am start -n org.mygo.host/org.mygo.MygoActivity
adb logcat -s mygo
```

#### 预期失败模式

| 现象 | 原因 / 处理 |
| --- | --- |
| `INSTALL_FAILED_NO_MATCHING_ABIS` | 模拟器是 x86_64；换 arm64 镜像 |
| `UnsatisfiedLinkError: dlopen failed: library "libmygo.so" not found` | `.so` 未进 APK；确认 `android/app/src/main/jniLibs/arm64-v8a/libmygo.so` 后重跑 `assembleDebug` |
| 启动后黑屏 | surface/EGL 问题；`adb logcat` 看 Go 日志与 EGL 报错 |
| `adb devices` 为空 | USB 未转发 / 未开 USB 调试 / 未授权 |

**验收标准**：`am start` 后 Activity 起来、`logcat -s mygo` 有 Go 侧日志、屏幕出现 Surface 内容，且无 `UnsatisfiedLinkError`。

## Phase 5 — 权限 / 常亮 / 设备信息 / 深链 / 语音（2026-10-22 完成）

Retty 的 Android 端要跟 iOS 对齐，补了五项能力。全部走第 3 节的同步模板，`CGO_ENABLED=0 go test ./internal/android/backend` 可覆盖的部分都加了单测。

| 能力 | 接口 | 说明 |
| --- | --- | --- |
| Permissions | `Permissions()` | 麦克风与 speech 共用 `RECORD_AUDIO`；通知在 13+ 是 `POST_NOTIFICATIONS`，13 以下看 `areNotificationsEnabled`；请求走 `0x5CAC`，结果用拉模式回传，10 分钟没回答按超时回调 |
| Power | `Power()` | `KeepAwake(display)` 用 `FLAG_KEEP_SCREEN_ON`，只保 CPU 时用 `PARTIAL_WAKE_LOCK`；`OnBattery` 读 sticky 电量广播；lease 释放一次幂等 |
| Device | `Mobile().Device()` | Java 侧回 UTF-8 JSON：用户设的设备名或厂商 + 型号、Android 版本 |
| Deep link | `AppHandler.OpenURLs` | `onCreate` / `onNewIntent` → `nativeOpenURL`；Init 前到达的 URL 先缓冲，装好投递口再冲 |
| Speech | `Speech()` + `androidSpeechProvider` | 系统 `SpeechRecognizer`；`speech.go` 现在按平台选默认 provider（iOS 用 Apple，Android 用系统） |

### 踩到的坑：JNI 方法表的签名必须与 Kotlin 逐字一致

`requestPermission` 的签名曾写成 `(JI)I`，而宿主是 `requestPermission(kind: Int, token: Long)` 即 `(IJ)I`。后果不是某个功能不可用，而是 **`attach` 直接失败（返回 -2），所有平台能力一起消失**；编译、单测、Kotlin 编译全部通过，只有真机才暴露。

现在两点防护：

- `lookups[]` 找不到方法时会用 `__android_log_print` 打出方法名与签名，logcat 直接指出要修哪一条；
- Retty 侧的 `scripts/check_android_bridge.py` 在打包后比对 JNI 表与 APK 里的 `classes.dex`，不一致就让 CI 失败。
