# Android 推送：国内优先，FCM 后置

> 状态：方案稿。S1 的 FCM 收包已实现（见 [android-smarthome-gaps.md](android-smarthome-gaps.md)），本文定的是**国内厂商通道**的接入方式。
> 最后核实：2026-10-23。

## 0. 结论

- 目标市场是国内，**FCM 不是主路径**：国产 ROM 普遍没有 GMS，或用户没有 Google 账号；App 被杀后只有厂商通道能唤醒。
- MyGo **不绑定任何厂商 SDK**。SDK 是闭源 AAR、每个应用一套 AppID/证书、还会往 manifest 里塞自己的 service/receiver——这些都不该进框架。
- MyGo 提供的是**与厂商无关的两个入口**（token 与消息），厂商 SDK 由每个 app 在自己的宿主副本里接：
  - `MygoPlatform.deliverPushToken(token)` → Go `App.OnPushToken`
  - `MygoPlatform.onPushMessage(context, id, title, body, data)` → 本地通知（含 urgency/actions/progress/深链点击）或 Go `App.OnPushMessage`
- 发送端（server → 厂商推送 API）属于应用后端，MyGo 不做。

## 1. 厂商矩阵

| 通道 | 覆盖 | SDK | 备注 |
| --- | --- | --- | --- |
| 华为 / 荣耀 | HMS 设备（含鸿蒙 3/4 的 Android 兼容层） | HMS Push Kit | 鸿蒙 5（NEXT）不跑 Android 应用，不在范围 |
| 小米 | MIUI / HyperOS | Mi Push | 系统内置服务，到达率最好的一档 |
| OPPO | ColorOS | OPPO PUSH | 需要应用商店/后台配置 |
| vivo | OriginOS | vivo Push | 同上 |
| 魅族 | Flyme | Flyme Push | 量小，可后置 |
| 聚合 | 上述全部 | 个推 / 极光 / 腾讯 TPNS | 一套 SDK 覆盖多厂商，但多一层第三方云与合规成本 |

选择建议：**先接一个聚合 SDK 或按用户设备分布选单厂商**，验证「杀进程后能收到」这条链路；多厂商路由放第二步。

## 2. 每个厂商 SDK 的接法（共性）

1. **依赖**：把 AAR 加进宿主工程的 `android/app/build.gradle`（每个 app 自己的副本，不进框架）。
2. **manifest**：按 SDK 文档复制它的 service/receiver/permission；这些组件名属于 SDK，不归 MyGo。
3. **初始化**：在 `MygoApplication.onCreate()` 里、**用户同意隐私政策之后**初始化（合规要求，不能提前）。此时 `libmygo.so` 还没加载，禁止碰 `Native.*`。
4. **拿 token**：SDK 注册成功回调里调 `MygoPlatform.deliverPushToken(regId)`，Go 侧 `OnPushToken` 收到后上报给应用后端，后端按 token 选择厂商接口发送。
5. **收消息**：
   - 通知型（厂商通道自动渲染）：系统直接显示，点击按 SDK 的约定打开应用；若需要深链，在 SDK 的点击回调里把目标 URL 放进启动 Intent，走现有 `OnOpenURL`。
   - 数据型（透传）：在 SDK 回调里取 title/body/data，调 `MygoPlatform.onPushMessage(...)`——**这会把消息变成一条本地通知**（进程被杀也能显示），或纯数据时交给 Go。
6. **点击**：与 FCM 共用 `MygoPlatform` 的通知与深链逻辑，不需要厂商特判。

## 3. MyGo 已经提供、与厂商无关的部分

- 通知渲染：通道分级（default / silent / alert）、进度条、action 按钮、点击回传。
- 深链：`data["url"]` → 点击 `ACTION_VIEW` 打开 Activity → 冷启动 `OnOpenURL`。
- 数据型消息：`nativePushMessage` → `App.OnPushMessage(map)`。
- token 回传：media 队列 kind 2/3 → `PushRegistered` → `OnPushToken` / `OnPushRegistrationError`。
- 前台服务：长连接/轮询场景用 `App.KeepRunning`（S4）。

## 4. 每个 app 要做的清单

- [ ] 选通道（聚合或单厂商），拿到 AppID/AppKey/证书。
- [ ] 把 SDK 加进宿主副本的 gradle + manifest。
- [ ] `MygoApplication.onCreate` 里做合规初始化。
- [ ] 注册回调 → `deliverPushToken`。
- [ ] 透传消息回调 → `onPushMessage`。
- [ ] 后端按 token 路由到对应厂商发送接口（或聚合平台的 API）。
- [ ] 真机验证：杀进程 → 收通知 → 点击冷启动到正确页面。

## 5. 合规与坑

- **隐私合规**：SDK 初始化必须在用户同意隐私政策之后；隐私政策要列出所用推送 SDK 与收集的信息。
- **自启动/关联启动**：部分厂商要求声明；MIUI/鸿蒙对后台管控更严，产品口径写「尽力而为」。
- **通知权限**：Android 13+ 仍是 `POST_NOTIFICATIONS` 运行时权限，与通道无关。
- **FCM 适配器**：`MygoFirebaseService` 保留，作为**海外**渠道的适配器；海外发布时再按 google-services 配置启用。国内包不要带 Firebase 依赖，避免无 GMS 设备上的初始化噪音。
- **不要在一个包里同时初始化多个厂商 SDK**：按设备厂商选择初始化（`Build.MANUFACTURER`），否则自启动与内存开销会叠加。

## 6. 路线

1. **P0**：单通道（聚合或用户量最大的厂商）打通「杀进程后收包 + 点击冷启动」。
2. **P1**：按 `Build.MANUFACTURER` 选择厂商 SDK，后端按 token 前缀/登记表路由。
3. **P2**：FCM（海外）按 google-services.json 启用，复用同一个 `onPushMessage`。

## 7. 与 S1 的关系

S1 已把「收包 → 通知/Go」这条链路做成与 FCM 无关的形态（`onPushMessage` 不再接收 `RemoteMessage`）。FCM 只是第一个适配器；国内通道接进来不需要改框架，只改各 app 的宿主副本与本文件第 4 节的清单。

## 8. 聚合 SDK 对比与回调映射

### 8.1 三家的取舍

| | 个推 | 极光 JPush | 腾讯 TPNS | 友盟 U-Push |
| --- | --- | --- | --- | --- |
| OEM 通道 | 华为/小米/OPPO/vivo/荣耀/魅族全覆盖 | 同上 | 同上 | 同上（华为/小米/OPPO/vivo/荣耀/魅族） |
| 接入成本 | 一个 AAR + 一个 receiver | 同上，文档与示例更多 | 同上 | 同上，但 SDK 自带统计，依赖更重 |
| 自有通道 | 有 | 有 | 有 | 有（阿里云通道） |
| 合规披露 | 需要（第三方云） | 同上 | 同上 | 同上，且含统计类设备信息采集 |
| 适合 | OEM 覆盖优先 | 文档/社区优先，排障快 | 已有腾讯云账号时 | 已经在用友盟统计时 |

友盟 U-Push 也是同构的聚合通道：`PushAgent.register` / `getRegistrationId` 给 token，`UmengMessageHandler.handleMessage` 收透传，`UmengNotificationClickHandler` 处理点击，映射到 §8.2 的四行与极光一一对应，所以框架侧不用改。它的取舍不在技术，而在**你已经在用什么**：友盟的主业是统计，Push 是其中一块，如果 App 本来就要接友盟统计，用 U-Push 能少一套 SDK、少一份隐私条目；如果只想要推送，极光的文档与排障资料更专注，SDK 也更轻。另外它的 SDK 会一并采集设备信息，隐私政策要写全，合规审查比纯推送 SDK 多一项。

建议：**先用极光做 P0**（示例多、JPushMessageReceiver 的回调语义清晰，排障快），把「杀进程收包 + 点击冷启动」打通；如果 OEM 通道到达率不达标，再评估个推。TPNS 只在已有腾讯云体系时考虑。这不是技术优劣结论，是接入与排障成本的取舍。

### 8.2 回调映射（四家同构）

| 环节 | 极光 | 个推 | TPNS | 友盟 | MyGo 入口 |
| --- | --- | --- | --- | --- | --- |
| 注册成功 | onRegister(registrationId) | onReceiveClientId(cid) | 注册回调 | register 回调 / getRegistrationId | MygoPlatform.deliverPushToken(id) |
| 透传消息 | onMessage 的 data | onReceiveMessageData | onTextMessage | UmengMessageHandler.handleMessage 的 raw | MygoPlatform.onPushMessage(ctx, id, title, body, data) |
| 点击通知 | SDK 启动 Activity 的 Intent | 同上 | 同上 | UmengNotificationClickHandler 的 extra | Intent 里的 mygo.url extra → OnOpenURL |
| 通知展示 | SDK 自己的样式 | 同上 | 同上 | 同上 | 不需要 MyGo 参与（系统/厂商渲染） |

点击这一行是 2026-10-23 新增的约定：MygoActivity 除了标准的 intent.data，也读 mygo.url extra。厂商 SDK 的点击 Intent 通常只带 extra，把深链放在这个键里即可复用现有的冷启动路由，不需要为每家写特判。

### 8.3 一个最小适配器（极光，示意）

不要照抄进框架——它是每个 app 宿主副本里的一段代码：

    class MyPushReceiver : JPushMessageReceiver() {
        override fun onRegister(context: Context, id: String) {
            MygoPlatform.deliverPushToken(id)
        }

        override fun onMessage(context: Context, message: JPushMessage) {
            val data = message.extra ?: emptyMap()
            MygoPlatform.onPushMessage(
                context,
                message.msgId ?: data["id"].orEmpty(),
                message.notificationTitle ?: data["title"].orEmpty(),
                message.notificationContent ?: data["body"].orEmpty(),
                data,
            )
        }
    }

清单里注册 receiver、MygoApplication.onCreate 里在同意隐私后 JPushInterface.init(this)，其余由 AAR 自带 manifest 合并。
