# Android BLE 支持评估

> 状态：**M1（扫描）+ M2（GATT 连接/读写/订阅）+ M3（配对/MTU）已实现，真机未验**。实现现状见 §10。上位文档 [android-smarthome-gaps.md](android-smarthome-gaps.md)。
> 最后核实：2026-10-23。结论基于本机 AOSP 模拟器与 Android 官方 API 文档，没有真机 BLE 环境。

## 0. 结论

- 对「类似米家的控制端」，**BLE 不是第一优先级**。局域网直连（S3）、远程推送（S1）、通知与前台服务（S2/S4）已经覆盖大多数设备的控制与提醒。
- 真正需要 BLE 的只有两类：**BLE-only 设备**（门锁、传感器、体脂秤等）与**配网**（把 Wi-Fi 凭据交给设备）。
- 技术上可行，但**必须在 Kotlin 侧实现**，Go 只做协议与状态机：宿主约 900–1400 行（扫描/连接/GATT/权限/事件队列），Go 侧 300–500 行，JNI 表 +5～8 条。
- **模拟器无法验证**：AOSP 模拟器没有蓝牙适配器（BluetoothAdapter 为 null）。需要真机 + 一个外设（第二台手机跑 GATT server、nRF Connect 模拟外设，或 ESP32）。
- 建议：**先不做**。等 S1（GMS 推送）验完、局域网发现接到真实 mDNS/SSDP 库之后，再按 M1→M2 排期。

## 1. 范围

做（central / GATT client）：

- 扫描（service UUID 过滤、超时、RSSI）、停止扫描；
- 连接/断开、服务发现；
- 读/写 characteristic，写 descriptor 开启 notification/indication；
- 连接状态、通知值、读写结果的事件回传。

不做或后置：

- peripheral 模式（手机当外设）；
- 完整的配对/bonding 管理（只暴露 createBond 与状态事件，配对 UI 交给系统）；
- MTU 协商与连接优先级（M3）；
- 后台常驻扫描（Android 8+ 限制；要常驻只能复用 KeepRunning 的前台服务）；
- PendingIntent 式被动扫描（语义是系统代扫，不是我们要的控制路径）。

## 2. 为什么不能只用 Go

- go-ble/ble 支持 Linux/macOS/Windows，**不支持 Android**：没有到 BluetoothGatt 的绑定。
- 任何 Go 方案都要 cgo 或 gomobile 绑定 Java，仍然绕不开宿主的权限与生命周期。
- 因此架构与扫码、权限一致：**Kotlin 持有 GATT 对象，Go 发命令、用 pending 队列收事件**。

## 3. 权限矩阵（minSdk 29）

| API | 权限 | 备注 |
| --- | --- | --- |
| 29–30 | ACCESS_FINE_LOCATION + BLUETOOTH + BLUETOOTH_ADMIN | 扫描要求定位权限，且系统定位开关打开 |
| 31+ | BLUETOOTH_SCAN + BLUETOOTH_CONNECT | 扫描可声明 neverForLocation 免定位；连接必须有 CONNECT |

- 运行时权限走现有 Permissions 模块：新增 kind `bluetooth`——31+ 一次请求两个，29–30 请求定位。
- manifest 里旧权限加 android:maxSdkVersion="30"，新权限只在 31+ 生效。

## 4. 建议的 Go API（草案）

低层、事件驱动；设备画像（门锁/灯/传感器）留给应用。

```go
type BLEDevice struct {
    Address  string
    Name     string
    RSSI     int
    Services []string // 仅连接并发现后填充
}

type BLEEventKind string

const (
    BLEAdvertisement BLEEventKind = "advertisement"
    BLEConnected     BLEEventKind = "connected"
    BLEDisconnected  BLEEventKind = "disconnected"
    BLEServices      BLEEventKind = "services"
    BLEValue         BLEEventKind = "value" // 读结果或通知
    BLEWriteDone     BLEEventKind = "written"
    BLEError         BLEEventKind = "error"
)

type BLEEvent struct {
    Kind           BLEEventKind
    Address        string
    Characteristic string // "<service-uuid>/<char-uuid>"
    Value          []byte
    Err            error
}

func (b *BluetoothModule) Scan(filter BLEScanFilter, timeout time.Duration) error
func (b *BluetoothModule) StopScan()
func (b *BluetoothModule) Connect(address string) error
func (b *BluetoothModule) Disconnect(address string)
func (b *BluetoothModule) Services(address string) ([]BLEService, error)
func (b *BluetoothModule) Read(address, characteristic string) error
func (b *BluetoothModule) Write(address, characteristic string, value []byte, withResponse bool) error
func (b *BluetoothModule) Subscribe(address, characteristic string, on bool) error
func (b *BluetoothModule) OnEvent(fn func(BLEEvent)) (off func())
```

- 所有操作**异步**，结果统一走事件：GATT 本身就是异步的，包成同步必然引入超时语义。
- 命令侧同步 JNI（返回是否受理），结果侧 pending 队列 + Go 轮询，与扫码/权限完全同构。

## 5. 宿主改动

- MygoPlatform.kt：BluetoothManager / BluetoothLeScanner / BluetoothGattCallback；新增 pendingBle 队列。
- JNI：bleScan、bleStopScan、bleConnect、bleDisconnect、bleRead、bleWrite、bleSubscribe、bleServices + takeBleEvent。lookups 表 +8～9 条。
- 权限：新增 kind 与 manifest 两套声明。
- 后台扫描：Android 8+ 对后台扫描有限制；要常驻只能靠 KeepRunning 的前台服务，且需要在通知里说明。
- **重连与 GATT 133 属于应用层**：宿主只报事件，不做自动重试——重试策略依赖设备协议。
- 序列化：特征值是二进制，不能用 JSON。事件编码用 kind\naddress\nuuid\nbase64(value)，与 media 队列的换行编码一致。

## 6. 量级拆分（里程碑，不排期）

| 里程碑 | 内容 | 量级 |
| --- | --- | --- |
| M1 | 权限 kind + 扫描/停止 + 广告事件 | 小～中（宿主 ~250 行，Go ~100 行） |
| M2 | 连接、服务发现、读/写、订阅 | 中（宿主 ~450 行，Go ~200 行） |
| M3 | bonding、MTU 请求、连接优先级 | 中（宿主 ~150 行） |
| M4 | 配网辅助（按厂商协议写 Wi-Fi 凭据） | 应用层，不进框架 |
| Matter | BLE 配网 + mDNS + Thread 凭据 + CASE | 大，独立立项 |

## 7. 验证方案

- **模拟器不行**：本机 AOSP 模拟器没有蓝牙适配器，BluetoothAdapter.getDefaultAdapter() 为 null；dumpsys bluetooth_manager 也没有控制器。
- 真机需要至少一个外设：第二台手机跑 GATT server、nRF Connect 的模拟外设，或 ESP32。
- 用例：扫描发现 → 连接 → 发现服务 → 读 → 写 → 订阅收通知 → 断开 → 重连。
- 失败模式要覆盖：权限拒绝、蓝牙关闭、设备不在范围、GATT 133、通知未使能。

## 8. 风险

- 权限碎片化（29–30 与 31+ 两套），MIUI/鸿蒙对后台扫描更严。
- GATT 栈本身不稳定（133、连接超时），应用层必须能重试；框架不承诺可靠连接。
- 通知事件频率高（每秒可达数十次），pending 队列要能合并：建议通知只保留最新值 + 序号，避免 Go 轮询跟不上。
- BLE 地址在 Android 10+ 默认随机化，跨重连不能假设地址稳定——设备身份应使用服务层标识。

## 9. 决策建议

1. **现在不做**。先把 S1 的 GMS 真机验证补完，把局域网发现接到真实的 mDNS/SSDP 库——这两条覆盖米家式控制端的大部分场景。
2. 若产品确实有 BLE-only 设备：按 M1→M2 走，只做扫描/连接/读写/订阅；bonding 与配网协议留在应用层。
3. Matter 不做，除非目标是跨生态控制器。

## 10. 实现现状（M1+M2，2026-10-23）

实际落地的 API 与 §4 草案有几处**有意不同**，以使用为主：

```go
type BLEDevice struct{ Address, Name string; RSSI int }
type BLEService struct{ UUID string; Characteristics []string }

func (b *BluetoothModule) Scan(timeout time.Duration, found func(BLEDevice)) error
func (b *BluetoothModule) StopScan() error
func (b *BluetoothModule) Connect(address string, timeout time.Duration) (*BLEConnection, error)

func (c *BLEConnection) Services() []BLEService
func (c *BLEConnection) Read(characteristic string) ([]byte, error)
func (c *BLEConnection) Write(characteristic string, value []byte, withResponse bool) error
func (c *BLEConnection) Subscribe(characteristic string, handler func([]byte)) error
func (c *BLEConnection) Unsubscribe(characteristic string) error
func (c *BLEConnection) Bond(timeout time.Duration) error
func (c *BLEConnection) Bonded() bool
func (c *BLEConnection) RequestMTU(size int) (int, error)
func (c *BLEConnection) MTU() int
func (c *BLEConnection) Close() error
```

- **不用每个操作都带地址**：控制端同一时刻只连一个外设，地址放在 `BLEConnection` 上比每次重复更不容易错。想并发的调用方自己持有两个连接对象会被 `Connect` 拒绝。
- **不在事件层暴露**：草案的 `OnEvent(fn)` 把 GATT 的异步泄漏给应用；实际由 Go 内部单泵把事件翻译成「读的返回值、写的回执、订阅回调、断开错误」，应用看到的是同步调用 + 回调。
- **值走 base64 而不是换行编码**：事件本来就是 JSON（kind/address/characteristic），base64 是 JSON 里放二进制最不容易出错的方式，也省掉自定义转义。
- **同步带超时**：`Connect` 默认 10 s，读写 5 s；断开时挂起的操作立即返回 `ErrBLEDisconnected` 而不是等超时。主线程调用不会冻 UI（等待时泵 `backend().Step()`）。
- **特征值 key 归一化**：`"180f/2a19"`、`"2a19"`、`"00002a19-0000-1000-8000-00805f9b34fb"` 指同一个特征值，便于订阅时不必关心平台回传的是长 UUID 还是短 UUID。
- **配对与 MTU**：`Bond` 默认等 30 s（用户在设备上确认的时间算在内），已配对的设备立刻返回；`Bonded()` 读最后一次广播状态。`RequestMTU` 限制在 23–517，返回设备同意的大小，`MTU()` 在协商前是 23。配对状态只能从 `ACTION_BOND_STATE_CHANGED` 广播拿，所以在 `install()` 时注册一个进程级接收器（33+ 用 `RECEIVER_NOT_EXPORTED`）。
- **未做**：自动重连、GATT 133 重试、写操作排队（Android 栈本身串行，第二次写直接报错）。

**验证**：模拟器只有失败路径（无外设）——连接不可达地址 5 s 超时返回 `ErrBLETimeout`，未挂死；`bleCharacteristicKey`、服务/值/写回执/断开/配对/MTU 六类事件路由有单测并跑过 `-race`；`attach ok`、桥接检查 59 项全对（新增 7 条方法）。**成功路径（连上真外设读、写、订阅、配对、MTU）仍未验**。
