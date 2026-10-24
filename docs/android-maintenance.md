# Android 维护笔记（daodao-mygo）

> 面向后续维护：环境、构建、验证、现状与坑。详细计划/决策见 `docs/android-capability-plan.md`，架构见 `docs/mobile-architecture.md`。
> 最近一次整理：2026-10-22（上游已合并到 0.6.2，`2fb2cd3`）。

## 1. 本机环境（实测）

| 项 | 值 |
| --- | --- |
| 系统 | Debian 13 (trixie) |
| JDK | 21（`/usr/lib/jvm/java-21-openjdk-amd64`；仓库无 openjdk-17） |
| Android SDK | `~/android`（`ANDROID_HOME`）；platforms;android-36、build-tools;36.0.0、ndk;28.2.13676358、platform-tools |
| Go | 1.27.1 |
| 代理 | `http://127.0.0.1:20122`（curl 读 env；Gradle/git 需显式传） |
| HarfBuzz | arm64 预编译在 `third_party/hb-build/src/lib/android`；x86_64 本地编在 `.../android-x86_64` |

一键自检：`bash scripts/android-doctor.sh`。脚本默认 SDK 为 `~/Android/Sdk`，本机装在 `~/android`，必须靠 `ANDROID_HOME` 覆盖。

## 2. 构建

```bash
export JAVA_HOME=/usr/lib/jvm/java-21-openjdk-amd64
export ANDROID_HOME="$HOME/android"

# 1) Go .so（示例：android-smoke）。arm64 与 x86_64 各一份，HarfBuzz 目录不同。
HB=$PWD/third_party/hb-build/src/lib/android
CGO_CFLAGS="-I$HB/include/harfbuzz -I$HB/include/freetype2" \
CGO_LDFLAGS="-L$HB/lib -lharfbuzz -lfreetype -lm -llog" \
GOOS=android GOARCH=arm64 CGO_ENABLED=1 CC=$NDK/.../aarch64-linux-android29-clang \
go build -buildmode=c-shared -o android/app/src/main/jniLibs/arm64-v8a/libmygo.so ./examples/android-smoke

# x86_64 把 HB 换成 android-x86_64、CC 换成 x86_64-linux-android29-clang

# 2) APK（代理环境必须带 -D 参数）
./android/gradlew -p android --no-daemon \
  -Dhttp.proxyHost=127.0.0.1 -Dhttp.proxyPort=20122 \
  -Dhttps.proxyHost=127.0.0.1 -Dhttps.proxyPort=20122 :app:assembleDebug
```

- `scripts/android-build.sh` 默认只编 `./internal/android`（arm64），可传包名；它会校验 16KB 页对齐
- `abiFilters` 在 `android/app/build.gradle`；模拟器需要 `x86_64`，正式包可只留 `arm64-v8a`

## 3. 模拟器与真机

- 模拟器（Windows 上运行）：`adb connect 127.0.0.1:5555` 后按普通设备操作
- 真机（WSL）：Windows 侧 `usbipd attach --wsl --busid <id>`；WSL 侧 `/dev/bus/usb/*` 需要 udev 规则或 `sudo chmod 666`
- 自绘 Surface 的无障碍树未接到 uiautomator：`uiautomator dump` 只见 FrameLayout，点击按坐标/截图判断

## 4. 当前实现状态

- `examples/android-smoke`：能力验收 app，每个按钮跑一项能力，结果同时写 logcat（tag `mygo`，前缀 `smoke:`）
- 已验证：Phase 0（挂起修复）、1（Haptic）、2（StatusBar）、3（Photos/Export，含成功/取消/多文件）
- Badge：**框架层不做**（Android 无标准角标 API），保持 `ErrUnsupported`；需要数字角标由应用自行处理
- Push：接线完成（拉队列 + `PushRegistered`），无 GMS 时 10s 超时回传错误；**真 token 待 GMS 设备 + 包名对齐**

关键文件：

| 位置 | 作用 |
| --- | --- |
| `internal/android/backend/platform.go` | `PlatformBridge` 接口（Go→Java 的能力清单） |
| `android/jni/jni_platform.go` | JNI 槽/方法表/C helper/`androidJNI` 实现/轮询 |
| `internal/android/backend/media.go` | 选图/导出的令牌与结果分发 |
| `internal/android/backend/dialogs_android.go` | 覆盖 `platform.Dialogs` 的两个移动选择器 |
| `internal/android/backend/push.go` | push token 路由到 `MobileHandler` |
| `internal/android/backend/paths.go` | 宿主报告的私有目录，`paths_android.go` 据此回答 `PathUserData` 等 |
| `internal/android/backend/network.go` | `Network.Status` 走 `networkStatus` 桥；`Watch` 每 5 秒问一次，变化才上报 |
| `internal/android/backend/app.go` | `OnLifecycle`（前后台）与 `SetBackHandler`/`Back`（返回手势）；状态在 ready 前到达，由 `markReady` 补发 |
| `android/app/src/main/java/org/mygo/MygoPlatform.kt` | Java 侧平台能力（剪贴板/通知/触觉/状态栏/选图/导出/推送/无障碍） |
| `examples/*/start_android.go` | Android app 入口：`jni.Start(run)` |

## 5. 设计约束（踩过的坑）

1. **`android` 同时满足 `linux` build tag**：`internal/gpu/gl/present.go` 会在 Android 下编译；任何 linux-only 符号都要有 Android 实现（`hasContext` 曾因此缺失）。
2. **跨 JNI 文本一律 `byte[]` UTF-8**，不要用 `String`（modified UTF-8 不能带 NUL，BMP 外字符会错）。
3. **Java→Go 没有回调通道**：异步结果走拉模式——Kotlin 存 pending 队列，Go 用 goroutine 轮询 `takeXxxResult`。
4. **request code**：SCAN `0x5CA9` / PHOTOS `0x5CAA` / EXPORT `0x5CAB`，在 `MygoActivity.onActivityResult` 统一分发。
5. **Activity 是 plain `Activity`**（非 `ComponentActivity`）：异步只能用 `startActivityForResult`，别迁基类。
6. **Firebase 不接 google-services 插件**（AGP 9 newDsl 兼容性）：JSON 放 `src/main/assets/`，用 `FirebaseOptions` 手动初始化。
7. **`google-services.json` 的包名是 `top.togoto.mygo`，宿主 `applicationId` 是 `org.mygo.host`**：真机拿 token 前需对齐（改 Firebase 项目或改 applicationId）。
8. **每次加 `PlatformBridge` 方法都要同步 `androidJNI` 与 `test/fakeBridge`**，否则 `go test` 直接编译失败。
9. **生命周期事件早于应用启动**：`onStart`/`onResume` 在首次 surface 回调里的 `run()` 之前到达，那时后端还没有 `AppHandler`。后端记住状态，`markReady` 时补发；否则应用永远停在 Inactive。
10. **`WindowOptions.Hidden` 在 Android 上无从实现**：Activity 的 surface 从 `setContentView` 起就存在，`platform.WindowOptions` 也没有 `Hidden` 字段，后端一拿到窗口就要画首帧（否则内容永远不会运行）。要"先隐藏"的应用只能先画自己的背景。
11. **不要相信 `SDK_INT`**：厂商兼容层会报一个自己没实现的版本。实测鸿蒙 3.0 平板报 API 31，`WindowInsets.Type` 类在、`systemBars()` 没有，调用点在 `onApplyWindowInsets` 里抛 `NoSuchMethodError`；那次调用没包 `try`，异常一路冒到回调外面，进程在第一帧遍历时直接死，日志里只有一行 `NoSuchMethodError`。规则：新 API 一律 `try` 调、失败退回**真能用**的旧接口，不要只吞异常。已知要这样写的有 `WindowInsets.Type`、`WindowInsetsController`、`preferredDisplayModeId`、`Surface.setFrameRate`、`OnBackInvokedDispatcher.registerOnBackInvokedCallback`；退路分别是 `systemWindowInset*`、`systemUiVisibility`、不设模式、不设帧率、`onBackPressed`。
12. **没有 adb 也要能拿到崩溃原因**：宿主在 `Thread.setDefaultUncaughtExceptionHandler` 里把堆栈写到 `files/crash.txt`，下次启动先读它并显示。这次正是靠它拿到 `NoSuchMethodError` 的，否则只能看到一个闪退。原生崩溃（SIGSEGV）写不出文件，这本身也是信息。
13. **Android 没有 home 目录**：`os.UserHomeDir` 给出 `/sdcard`（不可写），且宿主设置的 `HOME` 来不及被运行时的环境快照读到。目录只能由 `getFilesDir()`/`getCacheDir()` 经 `nativeOnPaths` 传进来。
14. **进入 app 会自动弹键盘**：整整一个 SurfaceView 冒充文本编辑器（`MygoView.onCheckIsTextEditor` 恒为 true），Android 13 起窗口首次拿到焦点就据此自动弹键盘——Go 侧一个可编辑元素都没有也一样，日志里是 `SHOW_AUTO_EDITOR_FORWARD_NAV`，而不是我们调的 `showSoftInput`。宿主 manifest 用 `adjustResize|stateHidden` 压住首次弹出；之后可编辑元素拿到键盘时 `MygoIme.setSoftKeyboard` 照常拉起。

## 6. 未完成 / 可继续

- Push 真 token：GMS 真机/镜像 + 包名对齐
- Badge：如产品确需数字角标，做成应用层可选（launcher 私有 intent 或 ShortcutBadger，接受 best-effort）
- x86_64 HarfBuzz 目前是手工构建（/tmp 脚本），可考虑给 `third_party/hb-build/build.sh` 增加 `android-x86_64` target

## 7. 文档索引

- `docs/android-capability-plan.md`：分阶段计划、决策（拉模式、单文件 Export、Badge 不做）、验收记录
- `docs/mobile-architecture.md`：iOS/Android 如何在原版 mygo 上叠加
- `android/README.md`：host 工程细节（Surface 生命周期、JNI 契约）
- `docs/mobile-progress.md`：更早的移动端进度记录
