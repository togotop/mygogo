# Android 智能家居控制端：缺口清单与任务拆解

> 仓库：`daodao-mygo-mobile`（`github.com/egoist/mygo` 的移动分支）
> 状态：S0–S4、BLE（M1–M3）、mDNS（含防风暴）、SSDP、Wi-Fi 扫描与配网、音量/媒体键、NFC 已实现并在模拟器上验证；只剩国内推送通道待接（需要厂商 AppKey），真机长连/外设项目未验。上一轮能力补齐见 [android-capability-plan.md](android-capability-plan.md)（已完成）。
> 最后核实：2026-10-23，基于 `77f868a3` 的源码逐项 grep 与阅读，非文档注释。
> 范围：类似米家的 **Android 控制端 App** 所需的宿主/框架能力。云端、协议栈、设备固件不在内（见 §5）。
> 结论先行：UI 与既有平台能力够用；当时缺的**四条通道**（推送闭环、后台存活、局域网发现、BLE）现在只剩推送没接（FCM 无 GMS、国内通道等 AppKey）；应用级初始化点（`MygoApplication`）与崩溃记录已落地。

## 0. 与 android-capability-plan.md 的关系

那份计划补的是「对标 iOS 已有 API」的 Android 空缺（Haptic、StatusBar、Photos、Export、Push 接线、Permissions、Power、Device、Deep link、Speech），全部已完成。本文面向**上游 iOS 也没有**的新增需求：以智能家居控制端为假想客户，决定 MyGo 框架层要不要、以什么形状长出这些能力。

## 1. 现状盘点（已核实）

### 1.1 可以直接用的

| 能力 | 位置 |
| --- | --- |
| 自绘 UI（列表/卡片/动画/深浅色/IME） | `ui` 包 + `internal/android/backend` |
| 本地通知（点击回传、冷启动路由、替换/清除） | `NotificationOptions`（modules.go:808）+ `MygoPlatform$NotificationReceiver` |
| 权限查询/申请/跳设置 | `permissions_android.go`，kind 表可扩展 |
| 扫码（外部相机，FileProvider 回传） | `ScannerModule` |
| 安全存储（Keystore） | `secrets.go` |
| SQLite 本地缓存 | `plugins/sqlite`（purego，无需 cgo） |
| 网络在线/计费状态 + 变化监听 | `network.go:11` `NetworkModule`（仅此而已） |
| 语音识别、KeepAwake、真实机型、深链、Haptic、StatusBar、剪贴板 | 上一轮补齐 |

### 1.2 四条通道的缺口（证据表）

| 通道 | 代码证据（2026-10-23） | 现状 |
| --- | --- | --- |
| 推送闭环 | `MygoPlatform.kt:2031` `onMessageReceived(message) {}` 空实现；token 侧已通（:409 拉取、:426 `deliverPushToken`→`deliverMedia(2,…)`；Go `push.go` 全文 25 行，仅 `rememberHandler`/`PushRegistered`） | **收包为零**：App 被杀后设备事件无法唤醒用户 |
| 后台存活 | 全仓库源码 grep：foreground service、`WorkManager`、`AlarmManager`、`BOOT_COMPLETED` **零命中**（仅 build 中间产物噪音） | 单 Activity，进程死则 Go runtime 死 |
| 应用级初始化点 | manifest 组件仅 4 个：`.MygoActivity`(:77)、`MygoPlatform$NotificationReceiver`(:90)、FileProvider(:97)、`.MygoFirebaseService`(:108)；**没有 Application 子类**；crash recorder 挂在 `MygoActivity.onCreate`(:60) | 进程被推送拉起时无 Activity，什么初始化都不会发生 |
| 局域网发现 | `MulticastLock`、`WifiManager`、`NEARBY_WIFI_DEVICES` 源码零命中 | mDNS/SSDP 组播收不到；拿不到 SSID；无法配网 |
| BLE | `bluetooth` 源码零命中 | 配网、BLE 设备、Matter 配对全不可行 |
| 通知交互 | `NotificationOptions` 只有 ID/Title/Subtitle/Body/Silent/Group/Badge/Data/Sender；Kotlin 通道仅两个（`CHANNEL_ID`/`CHANNEL_ID_SILENT`，MygoPlatform.kt:1100+） | 无 action 按钮、无进度、无分级通道 |

## 2. 结构性约束（动手前要认账）

1. **每个新 Android 组件要同步三处**：Kotlin 类、`jni_platform.go` 的 `lookups[]` 表（约 :433）、Go bridge。上一轮 permissions/speech 的先例可照抄，但成本随组件数线性涨。
2. **所有平台能力长在 `MygoPlatform.kt` 单个 object 里**（2032 行），且宿主是每个 app 一份手工副本（见各仓库 docs：升级 MyGo 要重新同步）。加 3 个组件意味着每个 app 的副本都要再同步一次。
3. **库加载时机**：`System.loadLibrary("mygo")` 在 `MygoIme`/`MygoInput` 的静态块（MygoIme.kt:46），随首个引用发生。**Application 期没有库**——Application 子类里只能做纯 Java 的事（crash recorder 用 `Log.getStackTraceString` 恰好满足），不能碰 `Native.*`。
4. Activity 是 plain `Activity()` 非 ComponentActivity（capability-plan §1.4 的决策继续有效）。
5. mDNS 的组播锁不释放会耗电+被系统杀，必须配对 acquire/release。

## 3. 缺口明细与设计决策

### 3.1 推送闭环（P0）

**现状（2026-10-23 更正）**：token 注册其实**已经完整**——`RegisterPushNotifications` → `androidMobile.RegisterPush` → JNI `mygo_platform_register_push` → Kotlin `registerPush()`（MygoPlatform.kt:396）→ `deliverMedia(2/3)` → `collectMediaResults` → `backend.PushRegistered` → `OnPushToken`/`OnPushRegistrationError`。此前只看了 25 行的 `push.go` 就判断“无实现”，是错的。真正空的是 `onMessageReceived`。

**已实现（S1）**：
- **通知型 payload**（title/body，来自 `notification` 块或 data）：Kotlin 直接发本地通知，进程被杀、无 Activity 也能弹；点击时 data 里有 `url` 就 `ACTION_VIEW` 打开 Activity，走现有冷启动 `OnOpenURL` 路径，与深链同一条。
- **数据型 payload**：经新导出 `nativePushMessage(byte[])` 直接交给 Go（`androidJNI` 导出 → `backend.PushMessage` → `App.OnPushMessage(map[string]string)`），带 handler 类型断言，不影响 iOS/桌面。**没有**复用 media 队列——那条队列是一次性轮询（5 分钟窗口、单条即返回），塞常驻推送要么空转要么丢消息。
- **通道分级**：`NotificationOptions.Urgent` → `platform.Notification.Urgent` → JNI `postNotification(..., urgent)` → `org.mygo.alert`（`IMPORTANCE_HIGH`）；FCM data 里 `urgency=high|alert` 也走这条。
- 点击回传**没有**复用 `OnNotificationClick`：它只传 id，Kotlin 发的通知在 Go 侧没有登记表，拿不到 Data（原文这条判断有误）。

**验证**：模拟器无 GMS（`pm list packages` 无 `com.google.android.gms`），FCM 端到端未验；已验：编译、`MygoPlatform.attach ok`、三个通道都建出来、桥接检查全匹配。

**国内优先（2026-10-23 调整）**：目标市场没有 GMS，FCM 只是第一个适配器、后置到海外。收包入口已改成与厂商无关的 `onPushMessage(context, id, title, body, data)`；token 入口 `deliverPushToken` 本来就不绑定厂商。华为/小米/OPPO/vivo 与聚合 SDK 的接法、合规与清单见 [android-push.md](android-push.md)。

**量级**：中。

### 3.2 Application 子类（P0，先做，半天级）

**动机**：给进程一个不依赖 Activity 的初始化点——推送拉起、定时任务拉起、开机拉起都要经过它。

**要补**：
- 新增 `MygoApplication.kt`：`onCreate` 里安装 crash recorder（从 `MygoActivity` **前移**，Activity 只保留「读文件 + 显示诊断页」）；其余什么都不做。
- manifest `<application android:name=".MygoApplication">`。
- 约束：此处 **禁止** 碰 `Native.*`（见 §2.3）；禁止做任何可能抛的初始化。

**验证**：`adb shell am force-stop` 后用推送/通知拉起进程，crash.txt 仍能写（kill 进程的自杀测试：`kill -SIGSEGV` 写不出文件属预期，文档已记）。

### 3.3 通知 actions（P1）

**已实现（S2）**：`NotificationOptions` 增加 `Progress *float64`（0–1，带确定进度条）与 `Actions []NotificationAction{ID, Label}`。JNI 的 `postNotification` 从「逐参数」改成**一个 JSON 对象**（`([B)I`）——按钮是列表，逐参数签名装不下，而且每加一个字段就要改一次 C 包装、lookup 表和 Kotlin。Kotlin 为每个 action 建 `Notification.Action` + 独立 `PendingIntent`；按下走 `nativeNotificationAction(id, action)` → `App.OnNotification` 事件（`Action` 字段），普通点击监听照旧触发。inline reply 后置。

**为什么是 P1**：「一键关灯」「查看摄像头」是智能家居通知的核心交互；`NotificationReceiver` 的冷启动路由骨架现成。

**量级**：小。

### 3.4 局域网发现（P1）

**已实现（S3）**：
1. `Network.Multicast(on bool) error`：持有 Wi-Fi 组播锁（`WifiManager.createMulticastLock`，关掉引用计数、自己管释放）。**这是 mDNS/SSDP 一切 Go 库的前置**，没有它组播包到不了应用。平台侧是可选能力（`platform.NetworkWifi` + 类型断言），桌面后端不实现、也不受影响。
2. `Network.Wifi() WifiInfo`（SSID/BSSID/Connected）：系统不给权限时返回空串而不是错误，调用方把「没有 Wi-Fi」和「无权限」当同一件事。
3. 权限：新增 `PermissionWifi`（API 33+ → `NEARBY_WIFI_DEVICES`，更老 → `ACCESS_FINE_LOCATION`），`geolocation` 也终于映射到 Android 的定位 kind；manifest 声明 `CHANGE_WIFI_MULTICAST_STATE`/`ACCESS_WIFI_STATE`（普通）+ `ACCESS_FINE_LOCATION`/`NEARBY_WIFI_DEVICES`（运行时）。
4. 配网（`WifiNetworkSpecifier`）仍后置。

**验证**：Go 构建/vet、Kotlin 编译、`attach ok`、桥接检查 38 项（新增的两条 JNI 方法已对上）。**运行时未验**：没有会调用这两个 API 的 app 场景——要真验组播收发，需要一个走 mDNS 的冒烟 app（`examples/android-smoke` 加两行按钮即可）。

**为什么是 P1**：米家最被诟病的就是「必须过云」；局域网直连是差异化卖点，且**不需要 BLE**。

**量级**：中。

### 3.5 后台存活（P2）

**已实现（S4）**：
- **前台服务**：`App.KeepRunning(reason)` 返回 release。Android 起 `MygoForegroundService`（`foregroundServiceType="connectedDevice"` + `FOREGROUND_SERVICE_CONNECTED_DEVICE`），常驻通知显示 reason；release 经泵线程停服务，Kotlin 侧按持有计数，最后一个 release 才真正 `stopService`（多个调用方互不误伤）。
- **定时通知**：`Notification.Schedule(delay)` 在 Android 用 `AlarmManager`：能精确就 `setExactAndAllowWhileIdle`，`SCHEDULE_EXACT_ALARM` 被用户关掉时退到 `setAndAllowWhileIdle`；到点由 `MygoPlatform$NotificationAlarmReceiver` 把**已经死掉的进程**拉起并发出通知（JSON 里先去掉 delay，避免重复排程）。
- 没做：`WorkManager` 周期任务与通用 `App.Schedule`（Go 回调在进程死亡时没有意义，场景逻辑应在应用层用服务器推送/本地数据重建）、`BOOT_COMPLETED` 自启。

**风险**：MIUI/鸿蒙的自启与后台管控会拦前台服务和闹钟，产品口径仍要写「尽力而为」。

**验证（2026-10-23，API 37 模拟器 + `examples/android-smoke`）**：`KeepRunning` 后 `dumpsys activity services` 显示 `isForeground=true foregroundId=1 types=0x00000010`；按 Home 8 秒后进程仍在、服务仍是前台；20 秒后 release，服务记录归零。`Notification.Schedule(20s)` 后 `am kill` 掉进程，20 秒后通知照常出现。

### 3.6 BLE（P3，独立评估）

**决策**：Kotlin GATT 实现 + `lookups[]` 扩展 + 回传走 pending 拉模式（照扫码先例，capability-plan §1.1），**优于**绑 Go BLE 库（gomobile 绑定质量参差，且绕不开宿主权限）。
**用途**：BLE 配网、BLE-only 设备（门锁/传感器）。
**评估**：[android-ble-evaluation.md](android-ble-evaluation.md)。

**M1 已实现（2026-10-23）**：`Bluetooth.Scan(timeout, found)` / `StopScan()`，Kotlin 侧 `BluetoothLeScanner` + 扫描队列（每地址只报一次、队列上限 512），权限 kind `bluetooth`（31+ 用 `BLUETOOTH_SCAN`，更老用定位权限），manifest 声明四类蓝牙权限并 `uses-feature required=false`。没有适配器返回 `ErrUnsupported`，没有权限返回 `ErrPermissionDenied`。
验证：模拟器（虚拟 Bluetooth 适配器）扫描 2 秒正常启停、无设备；**真实设备发现与后续 GATT 操作需要真机 + 外设**。

**M2 已实现（2026-10-23）**：`Bluetooth.Connect(addr, timeout)` 返回 `*BLEConnection`（内部等 `connected` 与 `services` 两个事件，任何一步失败立即返回），之后用 `Services()` / `Read` / `Write` / `Subscribe` / `Unsubscribe` / `Close`。特征值按 base64 传二进制；`withResponse=false` 走 `WRITE_TYPE_NO_RESPONSE` 且不等回执；订阅写 CCCD（`0x2902`），回调在写 CCCD **之前**登记，避免第一帧通知丢掉。

事件仍是 pending 拉队列，但 Go 侧改成**单泵**：一个 goroutine 同时投递扫描结果与 GATT 事件，扫描与连接都结束后自动停止轮询——空闲时不产生 JNI 调用。阻塞语义：`Connect/Read/Write` 在 Go 侧是同步带超时的；在主线程调用时用 `backend().Step()` 泵队列，UI 不会冻住。链路断开时挂起的读写立刻以 `ErrBLEDisconnected` 返回，不必等超时。

**未做（有意留应用层）**：同地址多连接（控制端一次一个）、自动重连、GATT 133 重试、操作排队（栈本身串行，并发第二次操作直接报错）。

**验证**：模拟器只有失败路径可验——连接 `00:00:00:00:00:00` 5 秒超时返回 `ErrBLETimeout`（进程未挂死）；特征值 key 归一化与事件路由有单测并行跑过 `-race`。**真实外设的连接/读/写/订阅仍需真机 + nRF Connect 之类的外设**。

**M3 已实现（2026-10-23）**：`conn.Bond(timeout)` / `Bonded()`（默认 30 s，用户在设备上确认也算在内；已配对的设备立刻返回），`conn.RequestMTU(size)`（23–517，返回实际协商值）/ `MTU()`。Kotlin 侧配对状态只能从 `ACTION_BOND_STATE_CHANGED` 广播拿（GATT 回调里没有），所以在 `install()` 里注册了一个进程级接收器（33+ 用 `RECEIVER_NOT_EXPORTED`）；MTU 走 `onMtuChanged` 回调。断开时挂起的配对/MTU 等待同样立即返回 `ErrBLEDisconnected`。

**下一步**：无（M4 配网协议属于应用层）。

### 3.7 其余

原先列在这张表里的四项（TTS、定位、App Widget、快捷设置磁贴）都已实现，细节见 §3.14–3.16 与下方条目：

- TTS：`Speech.Speak` / `StopSpeaking`（§3.16）。
- 定位：`Geolocation.Current()` 读系统最后一次定位（不主动请求，可能偏旧，`Time` 说明时刻）；地理围栏由应用层用 `KeepRunning` + 轮询组合。
- App Widget：`Widget.Set(WidgetContent{Title, Rows})` 推送标题 + 最多 4 行，点击行回传 `Widget.OnAction(id)`；布局是 RemoteViews（自绘 UI 放不进去），每 app 需复制 3 个 res 文件。
- 快捷设置磁贴：`Tile.Set` / `Tile.OnClick`（§3.15）。

仍然没做的只剩：

| 能力 | 用途 | 量级 |
| --- | --- | --- |
| 视频播放（摄像头类） | 无应用内相机预览、无解码管线；**短期 WebView/外部播放器绕过**，长期单独立项 | 大 |

### 3.21 NFC 读卡与写标签（已实现，模拟器无适配器只验了失败路径）

- `NFC.Available()`：有没有 NFC 适配器（不等于已打开）。`NFC.Scan(fn)` 返回 `stop`，用 Android 的**前台 reader mode**（`enableReaderMode`，A/B/F/V/Barcode 五种技术），只在界面位于前台时有效。
- `NFCTag{ID, Tech, Writable, Records}`；记录解析成 `NFCRecord{Type(text/uri/mime/external/unknown), Text, URI, MIME, Language, Raw}`。文本记录按 NDEF 状态字节处理 UTF-8/UTF-16 与语言码，URI 走 `NdefRecord.toUri()`。
- `NFC.WriteText(text)` 往**刚读到的、仍在感应区**的标签写一条文本记录（`Ndef.writeNdefMessage`）；标签已离开返回 `ErrNFCWrite`，只读标签返回 `ErrNFCTagReadOnly`。典型用法：在 `Scan` 回调里判断这个标签是不是「空白场景标签」，是就写下去。
- 权限：`android.permission.NFC`（安装即授予）+ `uses-feature android.hardware.nfc required=false`（没有 NFC 的设备也能装）。
- 平台侧同样是可选能力 `platform.NFCReader`，带编译期断言；桌面返回 `ErrUnsupported`。
- 验证（模拟器 API 37，2026-10-23）：`NFC.Available -> false`、`NFC.Scan -> not supported`（AOSP 模拟器没有 NFC 适配器），单测覆盖标签 JSON 解析与无读卡器时的降级。**真机读标签/写标签未验**。
- 原生指令：`NFC.Transceive(tech, data)` 向感应区里的标签发任意命令（`NfcA / NfcB / NfcF / NfcV / IsoDep / MifareUltralight / MifareClassic`），用于 NDEF 之外的标签——MIFARE 扇区、ISO-DEP APDU、Felica。用 `Scan` 读到的最后一张标签，所以要在回调里或标签离开感应区前调用；`Transceive` **不在主线程跑**（标签不应答会等满超时）。
- 卡模拟（HCE）：`NFC.Emulate(HCEOptions{AIDs, Timeout}, handle)` 让手机当卡，`handle` 收到 APDU 返回应答（通常数据 + `90 00`），返回 nil 或超时回 `6F00`。AID 在运行时注册（`CATEGORY_OTHER`），并把应用设为前台首选服务。宿主侧必须声明 `MygoHostApduService`（`exported=true` + `BIND_NFC_SERVICE` + `HOST_APDU_SERVICE` intent-filter + `@xml/apduservice`），两个宿主都已加；`apduservice.xml` 里预置的 `F04D59474F3031`（"MYGO01"）只在应用没开卡模拟时兜底回 `6F00`。
- 未做：写 URI/自定义记录（只做了文本）、`registerAidsForService` 之外的静态 AID 管理界面。

### 3.20 音量与媒体键（已实现）

- `Audio.Volume()` → `{Percent 0..100, Muted, Ringer}`；`Audio.SetVolume(0..100)`、`Audio.Adjust(±steps)`（一步等于一次音量键）、`Audio.SetMuted(on)`、`Audio.Press(MediaKey)`（`play-pause / play / pause / next / previous / stop / forward / rewind`）。
- 只动**媒体流**（`STREAM_MUSIC`），闹钟和铃声各自的音量不受影响；静音用 `ADJUST_MUTE/ADJUST_UNMUTE`，不是把音量设成 0，所以取消静音能回到原值。
- `Audio.SetRingerMode("normal"|"vibrate"|"silent")`：Android 6 起把铃声切成 silent 需要**勿扰访问权限**（`isNotificationPolicyAccessGranted`），没有就返回 `ErrDndAccess`；`Audio.SilenceAllowed()` 查状态，`Audio.OpenSilenceSettings()` 打开系统授权页（这一页只能用户点，应用代劳不了）。
- 媒体键用 `AudioManager.dispatchMediaKeyEvent` 发 DOWN+UP 两个事件，**不需要任何权限**，系统按普通媒体按钮处理。
- 平台接口是可选能力 `platform.AudioControl`（和 `NetworkWifi` / `WifiScanner` 一样），桌面后端不实现就返回 `ErrUnsupported`。
- 踩坑：Go 侧用类型断言取这个能力，方法名和接口不一致时**编译不报错、运行时静默降级成 unsupported**。已在 `androidMobile` / `androidNetwork` 上加 `var _ platform.X = …` 编译期断言，两边名字对不上直接编译失败。
- 验证（模拟器 API 37，2026-10-23）：读回 `33%, muted=false, ringer=normal`；`SetVolume(60)` 后读回 60%；`Press(play-pause)` 返回 ok；`SetRingerMode(vibrate)` 生效且读回 `vibrate`；`SetRingerMode(silent)` 在未授权时正确返回 `ErrDndAccess`（`SilenceAllowed=false`）。
- 未验：授权勿扰后 silent 真正生效、媒体键对真实播放器的作用（模拟器没有正在播放的媒体）。

### 3.19 SSDP / UPnP 发现（已实现）

- `SSDP.Search(service, timeout, found)`：`service` 为空即 `ssdp:all`；`found` 收到 `SSDPDevice{USN, Type, Location, Server, Addr, Seen}`。默认超时 3 秒。
- 协议面很窄，所以自己实现（`internal/ssdp`）：一次 M-SEARCH 组播到 `239.255.255.250:1900`（`MX: 2`），最多发 **2** 次（间隔 1.5 s，丢包常见、第三次没必要）；只认 `HTTP/1.1 200` 且带 `USN` 的答复，按 USN 去重；单包上限 8 KB、单次搜索最多 256 台，收尾把剩余窗口读完。
- **一台设备会答多条**：每答一个设备类型/服务类型就是一条不同的 USN（路由器常见 10+ 条）。要「一台设备一行」由应用按 UUID 前缀归并。
- Android 需要组播锁：`Search` 自己 acquire/release，与 mDNS 共用引用计数，不会重复持锁。
- 验证（模拟器 API 37，2026-10-23）：smoke 里起了一个本机应答器（验证收发与解析闭环），同时**真实网络里的华为光猫（192.168.2.1）也答了**，5 秒内共 13 条答复、正常结束。
- 未做：NOTIFY 被动监听（持续发现，需要长期占用组播）、抓 `description.xml` 与 SOAP 控制（UPnP 动作）、把自己宣传成设备。

### 3.18 Wi-Fi 扫描与配网（已实现）

- `Network.WifiNetworks(timeout)` → `[]WifiNetwork{SSID, BSSID, RSSI, Security}`；`Security` 是 `open / wep / wpa / wpa2 / wpa3 / enterprise` 之一，同一 SSID 的多个热点只报一个（系统结果里先出现的那个）。
- `Network.JoinWifi(WifiJoinOptions{SSID, Password, Security, Timeout})` 用 `WifiNetworkSpecifier` + `requestNetwork` 接入**设备自己开的热点**，连上后 `bindProcessToNetwork` 把整个进程的流量导过去（设备热点通常没有外网，请求里特意去掉 `NET_CAPABILITY_INTERNET`，否则系统会等到验证超时才认为连上）。用完必须 `Network.LeaveWifi()` 恢复默认路由。
- **系统确认框**：Android 11+ 前台应用调用这个 API 时，系统会弹「Connect to device / <SSID> / CANCEL / CONNECT」，用户点 CONNECT 才算数。应用要在前台调用，超时要按人的反应留足（默认 30 s）。
- 权限：扫描与入网要 kind `wifi`（33+ `NEARBY_WIFI_DEVICES`，更老 `ACCESS_FINE_LOCATION`）；manifest 还要 `CHANGE_WIFI_STATE`——**缺它 `startScan()` 直接抛 `SecurityException`**（模拟器实测，错误信息是 "Neither user … nor current process has android.permission.CHANGE_WIFI_STATE"），加上 `CHANGE_NETWORK_STATE` 给入网用。
- 事件走和 BLE 相同的拉队列：`networks` / `joined` / `join-failed` / `lost` / `error`，每次操作开始时清空，避免上一次的 `lost` 干扰下一次。
- 验证（模拟器，2026-10-23）：`WifiNetworks(8s)` 返回 `AndroidWifi 02:15:b2:00:00:01 -50 dBm open`；`JoinWifi` 触发了系统确认框（截图确认文案），未点 CONNECT 时按超时返回 `ErrWifiTimeout`；smoke 里加了两个按钮（扫描、JoinWifi→LeaveWifi）供真机手动验。
- **未验**：真正入网成功、`bindProcessToNetwork` 之后的 HTTP 可达性、真实设备热点。
- 未做：保存网络（`WifiNetworkSuggestion`）、忘记网络、配网协议本身（HTTP/BLE 传凭据在应用层）。

### 3.17 局域网服务发现（mDNS）（已实现）

- `MDNS.Browse(serviceType, found)` 浏览服务（`found` 在引擎 goroutine 上调用）；`MDNS.Advertise(service)` 应答自己的服务。仅 IPv4，PTR/SRV/TXT/A 四类记录，桌面与 Android 通用。
- **防风暴**（这是本项的重点）：
  - 发包走令牌桶：10 包/秒、突发 5；
  - 同一个包 1 秒内只处理一次（组播回环、话痨设备都会重复）；
  - 浏览只发 5 次查询（0/1/2/4/8 秒退避）然后停止，不做心跳式轮询；
  - 应答前随机抖动 20–120 ms，避免多设备同时说话；
  - 单个浏览最多跟踪 256 个服务、重复包表 1024 条上限，超过直接重置；
  - 大于 9 KB 的报文丢弃；A/AAAA 记录按地址去重，重复公告不会让地址列表增长；
  - **known-answer suppression**：后续查询把已知服务（剩余 TTL ≥ 一半）作为 PTR 放进查询的 answer 段，advertiser 看到就不再应答；
  - **TTL 过期**：按 PTR/SRV 的 TTL 记录过期时间，每次查询前清掉过期项；TTL=0 视为 goodbye，立即失效，设备离开后不会永远留在列表里；
  - **cache-flush 位**：SRV/TXT/A/AAAA 是单宿主记录，带 0x8000 位宣告；对端收到后先清掉该名字同类型的旧记录再写入，地址变更不会叠加。PTR 是共享记录，不带这个位；
  - **同名冲突**：收到同名 SRV 但端口或主机不是自己时，判定实例名被另一台设备占用，自动改名为「名字 (2)」并重新宣告；自身的记录（端口与主机都相同）不会误判；短时间内最多改名 10 次，避免来回抢名。
  - Android 的 Wi-Fi 组播锁按引用计数持有（第一个 Browse/Advertise 获取，最后一个释放）。
- 依赖：`github.com/miekg/dns` 负责报文编解码（新增的直接依赖）。
- 验证（模拟器，同进程 advertise + browse）：`MDNS found "Smoke" at [10.0.2.16]:9999`，重复公告后仍只发一次、地址不重复。
- 单测：internal/mdns/engine_test.go 覆盖 known-answer 判定、TTL 过期与 goodbye、地址去重、类型归一化。
- IPv6：AAAA 能解析；宣告时会带上本机的全局/ULA IPv6（link-local 因带 zone、当前记录表达不了而跳过）。
- 未实现：RFC 6762 的完整 probing 阶段（宣告前 3 次探测、250 ms 间隔）；当前是「先到先得 + 观察到冲突后改名」。跨设备发现需要第二台设备验证。

### 3.16 语音合成（TTS）（已实现）

- `Speech.Speak(text, SpeakOptions{Rate, Pitch, Queue})` 与 `Speech.StopSpeaking()`；Android 用系统 `TextToSpeech`，引擎后台启动：第一次可能返回 `ErrSpeechStarting`（稍后重试），设备没有引擎返回 `ErrUnsupported`。
- 桌面与 iOS 暂返回 unsupported（iOS 的 `AVSpeechSynthesizer` 可后续接）。
- 验证（模拟器无 TTS 引擎）：`Speech.Speak -> not supported on this platform`，重试同样；**成功朗读需要真机**。

### 3.15 快捷设置磁贴（已实现）

- `Tile.Set(TileContent{Label, Subtitle, Active})` 推送磁贴的标签、副标题与开关状态；`Tile.OnClick(fn)` 接收点击。
- 磁贴是 `TileService`：manifest 声明 + `BIND_QUICK_SETTINGS_TILE` 权限；点击只回传 Go，是否顺带打开应用由应用自己决定（磁贴不该默认拉起界面）。
- 每个 app 需要复制 `res/drawable/mygo_tile.xml` 与 manifest 的 service 条目。
- 验证（模拟器）：`cmd statusbar add-tile org.mygo.host/org.mygo.MygoTileService` → QS 出现「回家」磁贴；点它 → smoke 日志 `Tile.OnClick`；随后 `remove-tile` 清理。

### 3.14 无障碍（TalkBack）接线（已实现）

- 之前 Java 半段（AccessibilityNodeProvider、节点解析、动作回传）完整，但 Go 侧从不发树：ui 的 `rt.access` 永远是 false，TalkBack 只看到一个空白 View。
- 现在：`MygoPlatform.install` 检查 `AccessibilityManager.isEnabled`，开启时调 `nativeAccessibilityOn`；事件**不立即发**（那时可能还没有 Go 窗口，发了会丢），而是标记 pending，随第一个「有窗口」的帧发 `SurfaceEvent{AccessibilityOn}`。ui 之后每帧构建 AccessTree → `Surface.UpdateAccessibility` → JNI → Kotlin provider。服务在运行中开关也有监听。
- 验证（模拟器）：启用 `com.android.systemui.accessibility.accessibilitymenu` 服务后启动 smoke，日志依次出现 `accessibility on` 与 `accessibility tree: 9 nodes`；关闭服务后恢复。
- 未验：真实 TalkBack 的焦点遍历与动作（本机没有 TalkBack 包，只验证到节点树送达宿主 provider）。

### 3.13 系统代理（已实现）

- `Network.Proxy() ProxyInfo{Host, Port}`：读系统（或当前 Wi-Fi）配置的 HTTP 代理，直连返回空。
- Go 的 `net.Dial` 不会自动走代理：HTTP 客户端把它设进 `Transport.Proxy`，隧道协议要自己说 CONNECT。框架只负责把配置读出来，不替应用做路由决策。
- 实现：Kotlin `ProxySelector.getDefault()` → JSON；JNI 回传。桌面后端不实现，`Proxy()` 返回空。
- 验证（模拟器）：`settings put global http_proxy 10.0.2.2:8888` 后 smoke 输出 `Network.Proxy -> 10.0.2.2:8888`，随后清除设置。

### 3.12 EGL 失败自动回退（已实现）

- 之前只有「EGL 初始化失败 → 内存绘制」这一条；上下文正常但 `eglSwapBuffers` 不再出帧（驱动故障）会一直黑屏或冻结，只能靠 `--no-gpu` 重新打包。
- 现在两级判断，各只切换一次：
  1. `presentGPU` 连续 3 次 swap 失败 → 认为 GPU 帧到不了屏幕：`Native()` 返回空、发 `SurfaceRenew`，ui 改为内存绘制；
  2. `PresentPixels` 再连续 3 次失败 → 先 `eglDestroySurface`（窗口的 buffer 队列归 EGL 所有，必须先放掉），再用 `ANativeWindow` 窗口缓冲直接呈现。
- 验证：临时把 `eglSwapBuffers` 改成恒失败重新打包，日志依次出现 `the GPU's frames do not reach the display, drawing in memory` 与 `presenting through EGL failed 3 times, using the window buffer`，截图仍有 4.3% 深色像素（文字正常绘制）；恢复后正常路径无回退日志、画面正常。

### 3.11 截屏保护（已实现）

- `App.SetSecureScreen(on)` → `FLAG_SECURE`：截屏、录屏、最近任务缩略图都拿不到窗口内容，适合连接码、密钥、终端输出这类页面。
- 平台：Android 支持（`Activity` 的 window flag）；iOS/桌面返回 `ErrUnsupported`。
- 验证（模拟器）：开启后 `adb exec-out screencap` 得到全黑图（中心与四角均为 `#000000`）。

### 3.10 电池优化豁免（已实现）

- `Power.BatteryExempt() (bool, error)` 查询是否已被系统豁免；`Power.RequestBatteryExemption() error` 弹系统对话框（`ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS`），ROM 不接受时退到电池优化设置列表。
- manifest 增加 `REQUEST_IGNORE_BATTERY_OPTIMIZATIONS`。桌面/ iOS 返回「已豁免」，不弹任何东西。
- 背景：S4 的前台服务在 MIUI/EMUI 上仍可能被清理，这个豁免是用户可控的补救。
- 验证（模拟器）：对话框「Let app always run in background?」→ 点 Allow → `dumpsys deviceidle whitelist` 出现 `user,org.mygo.host`；重启应用后 `BatteryExempt -> true`。

### 3.9 系统字体缩放（已实现）

- 之前系统「字体大小」设置完全被忽略：宿主只上报 `densityDpi`，Go 侧排版不知道 `fontScale`。
- 现在 `MygoActivity.onCreate` 调 `Native.onFontScale(configuration.fontScale)` → `platform.SetFontScale`；`ui` 排版时把**文字大小**乘上它，间距/边框/控件尺寸不变（Android 里 sp 与 dp 的区别）。布局缓存比较的是乘过缩放的字号，所以缩放变化会让文字元素全部重排。
- 单测 `ui/fontscale_test.go`：缩放 2 时文字高度约翻倍，外层盒子的增量约等于文字增量。
- 模拟器实测：`settings put system font_scale 1.5` 后重启应用，logcat 出现 `onFontScale 1.50`。
- 未覆盖：显式设置了固定 `LineHeight` 的文本行高不会跟着放大（行高按 DIP 解释），需要时由应用按字号推导。

### 3.8 桌面小组件（已实现）

- Go：`Widget.Set(WidgetContent{Title, Rows []WidgetRow{ID, Label, Subtitle}})`（最多 4 行）、`Widget.Clear()`、`Widget.OnAction(fn func(id string))`。
- Kotlin：`MygoWidgetProvider`（AppWidgetProvider）+ `MygoPlatform.updateWidget/buildWidgetViews`；内容缓存在进程内，系统 `onUpdate` 时用缓存重建，没有缓存显示「MyGo」标题。
- 资源（每个 app 的宿主副本要复制）：`res/layout/mygo_widget.xml`、`res/xml/mygo_widget_info.xml`、`res/drawable/mygo_widget_bg.xml`；manifest 加 receiver + meta-data（见 fork 的 AndroidManifest）。
- **限制**：RemoteViews 只能用系统控件，Go 自绘的界面放不进去；4 行、纯文本是刻意的通用形态，更复杂的布局要 app 自己写 provider。
- 验证：provider 已注册（`dumpsys package`），`Widget.Set` 返回 ok；**桌面上的实际渲染与点行**需要手动把小组件拖到桌面后验证（本机 launcher 的 picker 无法用命令行稳定驱动）。

## 4. 分阶段计划

依赖关系：S0 → S1 → S2 → S3 → S4 → S5（S3 只依赖 S0；S5 独立）。

### Phase S0 — Application 子类（半天）— ✅ 2026-10-23
- 已完成：新增 `MygoApplication.kt`（安装 crash recorder、持有 `takeLastCrash`）；manifest `android:name=".MygoApplication"`；Activity 只保留「读 + 显示诊断页」。retty 与 neohm 已同步。
- 静态验证：APK 的 merged manifest 为 `org.mygo.MygoApplication`，类在 `classes3.dex`；retty/neohm 的 Kotlin 编译通过。
- 运行时验证（2026-10-23，x86_64 / API 37 模拟器）：`am crash --user 0 com.daodao.retty` 后进程退出，`files/crash.txt` 801 字节；重开显示诊断页、文件随即删除；再次启动正常、无诊断页。
- 回退：删 Application 注册即可，无 API 变化。

### Phase S1 — 推送闭环（P0）— 代码完成 2026-10-23；国内通道优先，FCM 后置
- 已完成：与厂商无关的 `onPushMessage`（通知型 → 本地通知 + 深链点击；数据型 → `nativePushMessage` → `OnPushMessage`）；`Urgent` → alert 通道；token 链路已有。FCM 适配器（`MygoFirebaseService`）是第一个实现。
- 已验：编译、attach、三通道、桥接检查。
- 待做：国内厂商通道（见 [android-push.md](android-push.md)）——选通道、接 SDK、杀进程收包与点击冷启动；FCM 留到海外发布。
- 回退：FCM 整体是可选编译路径（google-services.json 放 assets，不接插件）。

### Phase S2 — 通知 actions + 进度（P1）— ✅ 代码完成 2026-10-23
- 已完成：`Progress`/`Actions` 字段；`postNotification` 改 JSON 载荷；按钮 PendingIntent → `nativeNotificationAction` → `OnNotification`（`Action`）。
- 已验（2026-10-23，API 37 模拟器 + `examples/android-smoke`）：通知带 `android.progress=40`、一个 `OK` 按钮；点按钮后 Go 收到 `OnNotification id="smoke-action" clicked=true action="ok"`，通知随即消失。

### Phase S3 — 局域网发现（P1）— ✅ 代码完成 2026-10-23
- 已完成：`Network.Multicast`、`Network.Wifi`、`PermissionWifi` + `geolocation` 映射、manifest 四条权限。
- 已验（2026-10-23，API 37 模拟器 + `examples/android-smoke`）：连上 `AndroidWifi` 后 `Network.Wifi()` 返回 `{SSID:AndroidWifi BSSID:02:15:b2:00:00:01 Connected:true}`；`Multicast(true)` 后 `dumpsys wifi` 出现 `Multicaster{mygo uid=…}`，`Active lock owners` 计数随之增加。
- 待验：真实 mDNS/SSDP 收发（没有接发现库）、锁在 Activity 暂停时释放（目前靠进程退出）。
- 回退：可选能力，未实现的后端返回空/无操作，不影响既有能力。

### Phase S4 — 前台服务 + 定时（P2）— ✅ 代码与模拟器验证完成 2026-10-23
- 已完成：`App.KeepRunning(reason)`（`connectedDevice` 前台服务 + 持有计数）、`Notification.Schedule` 的 AlarmManager 实现（精确/退化为非精确，进程死亡仍可发）。
- 已验：服务 `isForeground=true types=0x00000010`、切后台存活、release 后停止；定时通知在 `am kill` 后仍到达。
- 待验：真机息屏 30 分钟的连接保持、Android 14+ 真机的前台服务类型检查（模拟器 targetSdk 36 已通过启动）。

### Phase S5 — BLE（M1 扫描 + M2 GATT + M3 配对/MTU 已代码完成，2026-10-23；真机未验）
- 已完成：M1 扫描（权限 kind + 事件队列）、M2 连接/服务发现/读/写/订阅（`BLEConnection`，单泵投递事件）、M3 `Bond`/`RequestMTU`。
- 已验：模拟器 `attach ok`（59 项桥接全对）、失败路径（连接超时、无适配器）与单测（含 `-race`）。
- 待验：真机 + 外设的完整用例（发现 → 连接 → 读 → 写 → 订阅 → 配对 → MTU → 断开 → 重连），以及 GATT 133 的表现。
- 未做：M4 配网协议（应用层）。评估见 [android-ble-evaluation.md](android-ble-evaluation.md)。

## 5. 不需要 MyGo 补的（应用/后端自理）

账号体系与 OAuth、设备影子与状态机、场景/自动化引擎、固件 OTA、Matter/Thread 协议栈、云侧 MQTT broker——这些是**服务端与应用逻辑**，Go 写没有障碍（`plugins/sqlite` 做本地缓存、WebSocket 用现成 Go 库；`plugins/websocket` 是 Web 前端的，与 native 无关）。

## 6. 总验收清单

- [x] S0：进程级 crash recorder 可写 crash.txt（`am crash` 实测）；诊断页一次即删；retty 启动/返回回归通过（neohm 只做了编译验证）
- [ ] S1：国内厂商通道杀进程收包 + 点击冷启动（FCM 后置；见 android-push.md）
- [x] S2：通知 action 按钮点击回传（存活场景实测：`action="ok"` + 通知收起；冷启动未测）
- [x] S3：SSID 与组播锁实测通过；真实 mDNS 收发与锁释放待验（跨设备）
- [x] 局域网发现补全：SSDP 搜索（真机网络里的路由器答了 13 条）、mDNS 防风暴、Wi-Fi 扫描与配网
- [x] S4：前台服务实测（前台/后台/释放）；定时通知在进程被杀后仍到达（真机息屏长连未测）
- [ ] 每阶段：`go vet` / `go test` / 桥接检查（retty 的 check_android_bridge.py 对宿主副本）/ 模拟器冒烟

## 7. 风险与坑

- 三处同步（§2.1）——任何一处漏改都是「编译过、attach 失败、能力全灭」的静默故障，`requestPermission` 签名那次就是先例。
- 跨 JNI 文本一律 `ByteArray`（UTF-8），别用 String（modified UTF-8 带 NUL/emoji 出错）；每次调用后清理 Java 异常。
- FCM 在无 GMS 设备（AOSP、部分鸿蒙）必挂——所有 Firebase 调用必须 try/catch 降级，现状已如此，新代码保持。
- 组播锁、前台服务、精确闹钟、BOOT 都是被 ROM（MIUI/鸿蒙）重点照顾的对象：文档与产品口径都要写「尽力而为」，不做保活承诺。
- Android 14 起前台服务不声明 `foregroundServiceType` 直接拒发，属于发布检查项而非运行时故障。
