package org.mygo

import android.annotation.SuppressLint
import android.app.Activity
import android.app.AlarmManager
import android.app.Notification
import android.app.NotificationChannel
import android.appwidget.AppWidgetManager
import android.bluetooth.BluetoothDevice
import android.bluetooth.BluetoothGatt
import android.bluetooth.BluetoothGattCallback
import android.bluetooth.BluetoothGattCharacteristic
import android.bluetooth.BluetoothGattDescriptor
import android.bluetooth.BluetoothManager
import android.bluetooth.BluetoothProfile
import android.bluetooth.le.BluetoothLeScanner
import android.bluetooth.le.ScanCallback
import android.bluetooth.le.ScanResult
import android.bluetooth.le.ScanSettings
import android.appwidget.AppWidgetProvider
import android.service.quicksettings.Tile
import android.speech.tts.TextToSpeech
import android.service.quicksettings.TileService
import android.app.Service
import android.app.NotificationManager
import android.media.AudioManager
import android.app.PendingIntent
import android.content.ActivityNotFoundException
import android.content.BroadcastReceiver
import android.content.ClipData
import android.content.ComponentName
import android.content.ClipDescription
import android.content.ClipboardManager
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.content.pm.PackageManager
import android.provider.MediaStore
import android.graphics.Rect
import android.graphics.drawable.Icon
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import android.util.Log
import android.location.Location
import android.location.LocationManager
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import android.net.NetworkRequest
import android.net.Uri
import android.net.wifi.WifiManager
import android.net.wifi.WifiNetworkSpecifier
import android.nfc.NdefMessage
import android.nfc.NdefRecord
import android.nfc.NfcAdapter
import android.nfc.NfcManager
import android.nfc.Tag
import android.nfc.cardemulation.CardEmulation
import android.nfc.tech.IsoDep
import android.nfc.tech.MifareClassic
import android.nfc.tech.MifareUltralight
import android.nfc.tech.Ndef
import android.nfc.tech.NfcA
import android.nfc.tech.NfcB
import android.nfc.tech.NfcF
import android.nfc.tech.NfcV
import android.os.Build
import android.os.Bundle
import android.os.Handler
import android.os.IBinder
import android.os.Looper
import android.os.PowerManager
import android.provider.Settings
import android.os.VibrationEffect
import android.os.Vibrator
import android.content.res.Configuration
import android.view.KeyEvent
import android.view.WindowInsets
import android.view.WindowInsetsController
import android.view.WindowManager
import android.view.View
import android.view.accessibility.AccessibilityEvent
import android.view.accessibility.AccessibilityManager
import android.view.accessibility.AccessibilityNodeInfo
import android.view.accessibility.AccessibilityNodeProvider
import android.widget.RemoteViews
import org.json.JSONArray
import org.json.JSONObject
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec
import java.io.File
import java.util.UUID
import com.google.firebase.FirebaseApp
import com.google.firebase.FirebaseOptions
import com.google.firebase.messaging.FirebaseMessaging
import org.mygo.bridge.R

/**
 * Android 平台能力的 Java 侧实现：剪贴板、打开 URL、通知、无障碍。
 *
 * =============================================================================
 * 与 Go 侧的分工（先说清楚，后面每一段都按这个走）
 * =============================================================================
 * · **Java → Go**：本 object 里 3 个 `external` 方法（nativeAttach /
 *   nativeNotificationClicked / nativeAccessibilityAction）。它们的 JNI 符号名由类名 +
 *   方法名决定，所以**类名必须逐字是 `org.mygo.MygoPlatform`**，且成员必须挂在
 *   **类本身**上而不能挂在 `companion object` 下 —— 后者会把 native 方法变成
 *   `MygoPlatform$Companion` 的成员，符号随之变成
 *   `Java_org_mygo_MygoPlatform_00024Companion_nativeXxx`，与 Go 侧 `//export` 对不上。
 *   用 Kotlin `object` 声明的 `external fun` 正好落在类上，实例方法 ⇒
 *   Go 侧第二参数是 jobject（不是 jclass），符号仍是 `Java_org_mygo_MygoPlatform_<方法名>`。
 *   （Native.java 走的是 Java 的 `private static native`，那是另一套：它的 6 个符号是
 *   `Java_org_mygo_Native_*`，与本文件无关，也不要动。）
 *
 * · **Go → Java**：下面那些**普通方法**（readClipboardText / openExternalUrl /
 *   postNotification / setAccessibilityTree ...）。Go 侧用 JNI GetMethodID 按
 *   **方法名 + 签名**取到它们再调用，名字和签名逐字写在
 *   `android/jni/jni_platform.go` 的 `mygo_platform_attach` 表里；
 *   两边必须一起改。**这些方法必须是 public**：JNI 的 GetMethodID 对 private
 *   成员在各家 ART 上行为不保证，public 是最省事的正确做法。
 *
 * =============================================================================
 * 🔴 字符串一律用 ByteArray(UTF-8) 收发，不用 String
 * =============================================================================
 * JNI 的 NewStringUTF / GetStringUTFChars 走的是 **modified UTF-8**：它不能带 NUL，
 * 且 BMP 之外的字符（emoji、不少 CJK 扩展字）在两侧会被拆成代理对再拼错。剪贴板正是
 * 这类字符最容易出现的地方，所以本文件所有跨 JNI 的文本参数/返回值都是 `ByteArray?`
 * （null = 该表示“没有”），由本文件 `String(bytes, Charsets.UTF_8)` 解码。
 * 这是有意为之，不是偷懒。
 *
 * =============================================================================
 * 🔴 线程：Go 侧从 pump 线程调进来，UI 相关一律 post 到主线程
 * =============================================================================
 * Go 调过来的线程是 Go 的（不是 Android 主线程）。ClipboardManager / NotificationManager
 * / startActivity 从任意线程调都合法，但 **View 上的操作**（announceForAccessibility、
 * sendAccessibilityEvent、读 getLocationOnScreen）必须在主线程，所以本文件用 [onUi]
 * 统一处理：已经在主线程就直接执行，否则 Handler.post。
 *
 * =============================================================================
 * 本波做到什么程度（不要当成都做完了）
 * =============================================================================
 * · 剪贴板：读写文本 + HTML（ClipData 一个 item 同时带两个表示），✅ 真机可验。
 * · 打开 URL：ACTION_VIEW，✅ 真机可验；scheme 白名单在 Kotlin 侧**再查一遍**（见下面）。
 * · 通知：渠道 + POST_NOTIFICATIONS 运行时权限 + 点击回传，✅ 真机可验（需 Manifest 权限）。
 * · 无障碍：AccessibilityNodeProvider + 节点 JSON→AccessibilityNodeInfo + 动作回传 +
 *   announcement，代码完整；但**上游还差两处接线**（见文件末 mygo_platform_attach 需要的
 *   补丁清单）：① Go 侧 surface.UpdateAccessibility 未接（surface.go 是别人的面）；
 *   ② Android 侧还没有谁把 AccessTree 交给后端。⇒ 现状是「节点可被读」这条链路的
 *   Java 半段全部就位、Java 侧可单测（喂 JSON 就能拿节点），端到端未通。**不夸大。**
 */
// 前台服务使用的常量放在文件级：服务是顶层类（manifest 用名字直接引用），
// object 里的私有常量它拿不到。
private const val CHANNEL_ID_BACKGROUND = "org.mygo.background"
private const val FOREGROUND_ID = 1
private const val EXTRA_FOREGROUND_REASON = "org.mygo.FOREGROUND_REASON"

/**
 * 前台服务：一条常驻通知 + 进程不被回收。类型是 connectedDevice —— 智能家居/
 * 局域网场景就是「连着设备」，Android 14 起必须与 manifest 的声明和
 * FOREGROUND_SERVICE_CONNECTED_DEVICE 权限一致。
 */
class MygoForegroundService : Service() {
    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        val reason = intent?.getStringExtra(EXTRA_FOREGROUND_REASON) ?: "mygo"
        val nm = getSystemService(Context.NOTIFICATION_SERVICE) as? NotificationManager
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O &&
            nm?.getNotificationChannel(CHANNEL_ID_BACKGROUND) == null
        ) {
            nm?.createNotificationChannel(
                NotificationChannel(
                    CHANNEL_ID_BACKGROUND,
                    "mygo (background)",
                    NotificationManager.IMPORTANCE_LOW,
                ),
            )
        }
        val icon = applicationInfo.icon.takeIf { it != 0 } ?: android.R.drawable.ic_dialog_info
        val builder = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            Notification.Builder(this, CHANNEL_ID_BACKGROUND)
        } else {
            Notification.Builder(this)
        }
        builder.setContentTitle("mygo").setContentText(reason).setSmallIcon(icon).setOngoing(true)
        startForeground(FOREGROUND_ID, builder.build())
        return START_NOT_STICKY
    }

    override fun onDestroy() {
        stopForeground(true)
        super.onDestroy()
    }
}

/**
 * 快捷设置磁贴：标签、副标题与开关状态由 Tile.Set 推送；点一下交给 Go 的
 * Tile.OnClick。磁贴本身不启动应用，应用是否因此打开由应用自己决定。
 */
class MygoTileService : TileService() {

    override fun onStartListening() {
        val tile = qsTile ?: return
        val content = MygoPlatform.tileContent()
        tile.label = content?.optString("label")?.takeIf { it.isNotEmpty() } ?: "MyGo"
        tile.subtitle = content?.optString("subtitle").orEmpty()
        tile.state = if (content?.optBoolean("active") == true) Tile.STATE_ACTIVE else Tile.STATE_INACTIVE
        tile.icon = Icon.createWithResource(this, R.drawable.mygo_tile)
        tile.updateTile()
    }

    override fun onClick() {
        MygoPlatform.tileClicked()
    }
}

/**
 * 桌面小组件：只做「标题 + 最多 4 行」的通用形态，内容由 Widget.Set 推送。
 * RemoteViews 只能用系统控件，Go 自绘的界面放不进来 —— 这是平台限制，不是取舍。
 *
 * 点击一行会发一条广播回到这里，再交给 Go 的 Widget.OnAction。
 */
class MygoWidgetProvider : AppWidgetProvider() {

    override fun onUpdate(context: Context, manager: AppWidgetManager, ids: IntArray) {
        val views = MygoPlatform.buildWidgetViews(context)
        for (id in ids) {
            manager.updateAppWidget(id, views)
        }
    }

    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action == ACTION_ROW) {
            val row = intent.getStringExtra(EXTRA_ROW).orEmpty()
            if (row.isNotEmpty()) {
                MygoPlatform.widgetAction(row)
            }
            return
        }
        super.onReceive(context, intent)
    }

    companion object {
        const val ACTION_ROW = "org.mygo.WIDGET_ROW"
        const val EXTRA_ROW = "org.mygo.WIDGET_ROW_ID"
    }
}

object MygoPlatform {

    init {
        // 同 MygoIme：install() 与通知 receiver 都可能在任何 Native.* 之前被调用，
        // 不能只靠 Native 的静态块（幂等，同 MygoInput）。
        System.loadLibrary("mygo")
    }

    // ─────────────────────────── 状态 ───────────────────────────

    /** 宿主 Activity；install 时记下，UI 线程外的操作靠它 post。 */
    @Volatile
    private var activity: Activity? = null

    /** 承载 Go 渲染的 View（SurfaceView）；无障碍 provider 挂在它上面。 */
    @Volatile
    private var host: View? = null

    /**
     * 进程级的 Context（Application），install 时记下。没有 Activity 的宿主
     * （游戏引擎跑服务层）靠它起服务、发通知——这些只需要 Context，不需要界面。
     */
    @Volatile
    private var appContext: Context? = null

    /** 主线程 Handler；懒建，避免 install 之前就碰 Looper。 */
    private val mainHandler by lazy { Handler(Looper.getMainLooper()) }

    private const val CHANNEL_ID = "org.mygo.default"
    private const val CHANNEL_ID_SILENT = "org.mygo.silent"
    private const val CHANNEL_ID_ALERT = "org.mygo.alert"
    private const val NOTIFICATION_ID = 0
    private const val ACTION_NOTIFICATION_CLICK = "org.mygo.NOTIFICATION_CLICK"
    private const val ACTION_NOTIFICATION_ACTION = "org.mygo.NOTIFICATION_ACTION"
    private const val ACTION_NOTIFICATION_ALARM = "org.mygo.NOTIFICATION_ALARM"
    private const val EXTRA_NOTIFICATION_JSON = "org.mygo.NOTIFICATION_JSON"
    private const val EXTRA_NOTIFICATION_ID = "org.mygo.NOTIFICATION_ID"
    private const val EXTRA_NOTIFICATION_ACTION = "org.mygo.NOTIFICATION_ACTION_ID"

    // ─────────────────────────── 安装 ───────────────────────────

    /**
     * 由 Activity 在 onCreate 里调用一次：记下 Context 与 View、建通知渠道、通知 Go 侧
     * 「Java 侧就绪」。
     *
     * `nativeAttach()` 是本文件与 Go 侧唯一一次「握手」：Go 侧要靠它拿到 JavaVM，之后
     * Go 才调得动本文件的那些方法。**必须在任何平台能力被使用之前调用**，否则 Go 侧报
     * `ErrPlatformBridge`（而不是静默失败）。第二次调用是幂等的，但换 Activity 会让旧引用
     * 一直留着，所以这里只记最新的。
     */
    fun install(activity: Activity, host: View) {
        installInternal(activity, activity, host)
    }

    /**
     * 没有 Activity 的安装：给"窗口归别人所有"的宿主用（游戏引擎跑 MyGo 服务层）。
     *
     * 只装进程级的东西：通知渠道、蓝牙 bonding 广播、以及 Go 侧的握手。**不碰无障碍**——
     * 那要挂在 MyGo 自己的 SurfaceView 上，装到别人的视图上会抢走宿主的语义。
     * Activity 之后可以用 [attachActivity] 补上（拿前台服务、权限框要用）。
     */
    fun install(context: Context) {
        installInternal(context, null, null)
    }

    /**
     * 宿主接收 Go 消息的出口（应答与事件）。游戏引擎在自己的插件里注册，把 JSON 转成
     * Godot 信号；MyGo 自己的宿主不注册，消息就丢掉（只记一条日志）。
     */
    @Volatile
    private var hostSink: ((String) -> Unit)? = null

    /** 注册/清除 Go 消息的出口；clear 用 null。 */
    fun setHostSink(sink: ((String) -> Unit)?) {
        hostSink = sink
    }

    /**
     * Go 侧推一条 JSON 回来（应答或事件）。0 = 已交给宿主，1 = 没有宿主接收。
     *
     * 由 Go 的泵线程调用，所以**不保证在主线程**：Godot 插件要在自己的实现里
     * post 到主线程再 emitSignal。
     */
    fun hostEvent(json: ByteArray): Int {
        val text = String(json, Charsets.UTF_8)
        val sink = hostSink
        if (sink == null) {
            Log.w("mygo", "hostEvent dropped (no sink): " + text)
            return 1
        }
        sink(text)
        return 0
    }

    /**
     * 补上 Activity（宿主自己创建的 Activity 到来时调用）。与 install 的引用语义一致：
     * 只记最新的一个，旧的由 [uninstall] 或下一次 install/attach 替换。
     */
    fun attachActivity(activity: Activity) {
        this.activity = activity
        if (!accessibilityListening && host != null) {
            listenForAccessibility(activity)
        }
    }

    private fun installInternal(context: Context, activity: Activity?, host: View?) {
        // A new Activity means whatever the old one had open is gone: the
        // permission dialog was dismissed with it, and a recognizer bound to it
        // is dead. Clearing both keeps the next request from being refused as
        // "a prompt is already up" for the life of the process.
        permissionPromptUp = false
        pendingPermissionToken = 0
        releaseRecognizer()
        this.appContext = context.applicationContext
        this.activity = activity
        this.host = host
        createNotificationChannel(context)
        nativeAttach()
        if (host != null && activity != null) {
            host.accessibilityDelegate = MygoAccessibilityDelegate()
            listenForAccessibility(activity)
            notifyAccessibilityOn()
        }
        // 配对状态只有广播能拿到，GATT 回调里没有。进程级监听一次，配网/门锁
        // 这类需要 bonding 的设备才用得到。
        listenForBondState(context)
    }

    // listenForAccessibility 监听无障碍开关：TalkBack 等打开后，Go 侧才开始描述每一帧；
    // 服务可能在运行中被打开或关闭，所以监听状态变化，而不是只在启动时看。
    private fun listenForAccessibility(activity: Activity) {
        if (accessibilityListening) {
            return
        }
        accessibilityListening = true
        val am = activity.getSystemService(Context.ACCESSIBILITY_SERVICE) as? AccessibilityManager
        am?.addAccessibilityStateChangeListener { enabled ->
            accessibilityAnnounced = false
            if (enabled) notifyAccessibilityOn()
        }
    }

    /** Activity 销毁时调用：放开对 Activity/View 的引用。Go 侧不需要额外的 detach。 */
    fun uninstall() {
        activity = null
        host = null
        clearAccessibilityTree()
    }

    // ─────────────────────────── 剪贴板 ───────────────────────────

    /**
     * 读剪贴板文本，null 表示「没有文本表示」。
     *
     * 🔴 **Android 10（API 29）起的后台读取限制**：只有当前获得焦点的应用能读剪贴板，
     * 后台应用拿到的是 null，而且**与「剪贴板为空」在 API 上无法区分**
     * （hasPrimaryClip() 同样返回 false）。系统日志会写：
     *   `W/ClipboardService: Denying clipboard access to <pkg>, application is not in
     *    focus nor is it a system service for user 0`
     * 这不是本实现的 bug，是平台规则：调用方要在自己的窗口有焦点时读。
     */
    fun readClipboardText(): ByteArray? {
        val item = primaryClipItem() ?: return null
        val text = item.text ?: return null
        return text.toString().toByteArray(Charsets.UTF_8)
    }

    /** 读剪贴板的 HTML 表示，null 表示没有。 */
    fun readClipboardHtml(): ByteArray? {
        val item = primaryClipItem() ?: return null
        val html = item.htmlText ?: return null
        return html.toByteArray(Charsets.UTF_8)
    }

    /**
     * 剪贴板 clip 自己声明的 MIME 类型，每行一个、以 `\n` 拼接，null 表示读不到。
     *
     * 为什么是「一行一个的字节串」而不是 `Array<String>`：MIME 类型（RFC 6838）不含
     * 换行，而 Go 侧只要一种 JNI 形状（byte[]），返回字符串数组会多出一整套
     * GetObjectArrayElement/GetStringUTFChars 的胶水。这是刻意的取舍。"
     */
    fun clipboardMimeTypes(): ByteArray? {
        val cm = clipboardManager() ?: return null
        val description: ClipDescription = cm.primaryClipDescription ?: return null
        val mimes = ArrayList<String>(description.mimeTypeCount)
        for (i in 0 until description.mimeTypeCount) {
            val mime = description.getMimeType(i)
            if (!mime.isNullOrEmpty()) mimes.add(mime)
        }
        return mimes.joinToString("\n").toByteArray(Charsets.UTF_8)
    }

    /**
     * 写剪贴板：一个 clip、一个 item，可同时带 text/plain 与 text/html。
     *
     * 两个参数都可为 null（= 该表示不存在），但**至少一个非 null**；两个都是 null 时返回
     * false（Go 侧的空 Data 走的是 clearClipboard）。空字符串是合法值，与 null 语义不同，
     * 这一点由 Go 侧的 hasPlain/hasHTML 显式传递，本方法不做任何 trim。
     */
    fun writeClipboard(plain: ByteArray?, html: ByteArray?): Boolean {
        val cm = clipboardManager() ?: return false
        val text = plain?.let { String(it, Charsets.UTF_8) }
        val markup = html?.let { String(it, Charsets.UTF_8) }
        if (text == null && markup == null) return false
        return try {
            val mimes = ArrayList<String>(2)
            if (text != null) mimes.add("text/plain")
            if (markup != null) mimes.add("text/html")
            val item = ClipData.Item(text, markup)
            cm.setPrimaryClip(ClipData(ClipDescription("mygo", mimes.toTypedArray()), item))
            true
        } catch (e: RuntimeException) {
            // setPrimaryClip 在异常输入下会抛（例如 SecurityException：后台应用在
            // 某些 OEM 上不允许写）；返回 false 让 Go 侧报错，而不是把异常留在 JNI 线程上。
            false
        }
    }

    /** 清空剪贴板。 */
    fun clearClipboard(): Boolean {
        val cm = clipboardManager() ?: return false
        return try {
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {
                cm.clearPrimaryClip()
            } else {
                // API 24..27 没有 clearPrimaryClip：置一个空文本 clip 等价于「没有东西」。
                cm.setPrimaryClip(ClipData.newPlainText("", ""))
            }
            true
        } catch (e: RuntimeException) {
            false
        }
    }

    private fun clipboardManager(): ClipboardManager? {
        val ctx = activity ?: return null
        return ctx.getSystemService(Context.CLIPBOARD_SERVICE) as? ClipboardManager
    }

    private fun primaryClipItem(): ClipData.Item? {
        val cm = clipboardManager() ?: return null
        val clip = try {
            cm.primaryClip
        } catch (e: RuntimeException) {
            // 后台读取被拒时这里可能是 SecurityException（各家 ROM 行为不同），
            // 语义与 null 一致：「读不到」。
            return null
        }
        if (clip == null || clip.itemCount == 0) return null
        return clip.getItemAt(0)
    }

    // ─────────────────────────── 触觉 ───────────────────────────

    /**
     * 触觉反馈。kind 是 Go 侧 HapticFeedback 的序号：
     * 0 selection / 1 light / 2 medium / 3 heavy / 4 soft / 5 rigid /
     * 6 success / 7 warning / 8 error。
     * 返回 0 = 已发出；1 = 设备不支持（没有马达）；2 = 调用失败。
     */
    /**
     * 状态栏样式与可见性。style：0 auto / 1 light（浅色文字）/ 2 dark（深色文字）。
     * 返回 0 = 已应用；2 = 没有 Activity。
     */
    fun setStatusBar(style: Int, hidden: Boolean): Int {
        val act = activity ?: return 2
        onUi {
            val w = act.window
            // API 30 之前的 flags，也是给「报了 30 以上却没实现」的兼容层的
            // 退路：鸿蒙 3.0 报 31，但 WindowInsetsController 这一套会抛。
            fun legacy() {
                @Suppress("DEPRECATION")
                var flags = w.decorView.systemUiVisibility
                @Suppress("DEPRECATION")
                val full = View.SYSTEM_UI_FLAG_FULLSCREEN
                @Suppress("DEPRECATION")
                val light = View.SYSTEM_UI_FLAG_LIGHT_STATUS_BAR
                flags = if (hidden) flags or full else flags and full.inv()
                flags = if (appearanceLight(style)) flags or light else flags and light.inv()
                @Suppress("DEPRECATION")
                w.decorView.systemUiVisibility = flags
            }
            if (Build.VERSION.SDK_INT < Build.VERSION_CODES.R) {
                legacy()
                return@onUi
            }
            try {
                val c = w.insetsController ?: return@onUi
                if (hidden) c.hide(WindowInsets.Type.statusBars())
                else c.show(WindowInsets.Type.statusBars())
                val mask = WindowInsetsController.APPEARANCE_LIGHT_STATUS_BARS
                c.setSystemBarsAppearance(if (appearanceLight(style)) mask else 0, mask)
            } catch (t: Throwable) {
                Log.w("mygo", "WindowInsetsController 不可用，改用旧 flags: " + t)
                legacy()
            }
        }
        return 0
    }

    private fun appearanceLight(style: Int): Boolean = when (style) {
        1 -> false
        2 -> true
        else -> {
            val mode = activity?.resources?.configuration?.uiMode ?: 0
            mode and Configuration.UI_MODE_NIGHT_MASK != Configuration.UI_MODE_NIGHT_YES
        }
    }

    /**
     * Whether a network the app can use is up, as bit 0, and whether it is
     * metered, as bit 1. The Go side reads it as Network.Status, which is what
     * tells "there is no network" from "the server did not answer".
     *
     * A network counts as up when the system marks it for internet access: a
     * home network without a way out still counts, which is what an app that
     * talks to a device on it needs.
     */
    fun networkStatus(): Int {
        val ctx = activity ?: return 0
        val cm = ctx.getSystemService(Context.CONNECTIVITY_SERVICE) as? ConnectivityManager ?: return 0
        var v = 0
        val caps = cm.getNetworkCapabilities(cm.activeNetwork)
        if (caps != null && caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)) {
            v = v or 1
        }
        if (cm.isActiveNetworkMetered) {
            v = v or 2
        }
        return v
    }

    /** 持有前台服务的调用数；最后一个 release 才真正停服务。 */
    private var foregroundHolders = 0

    /** 磁贴当前内容，JSON；null 表示还没设置过。 */
    @Volatile
    private var tileJSON: String? = null

    // ─────────────────────────── BLE ───────────────────────────

    /** Client Characteristic Configuration Descriptor：开/关通知要写它。 */
    private val bleCCCD: UUID = UUID.fromString("00002902-0000-1000-8000-00805f9b34fb")

    /** 扫描事件队列：Go 侧每 120 ms 取一条。 */
    private val pendingBle = ArrayDeque<String>()

    /** 一次扫描里每个地址只报一次，广播再密也不会淹没 Go 侧。 */
    private val bleSeen = java.util.concurrent.ConcurrentHashMap.newKeySet<String>()

    private var bleScanner: BluetoothLeScanner? = null

    /**
     * 开始 BLE 扫描。0 = 已开始；1 = 没有权限；2 = 没有蓝牙或未开启；
     * 3 = 其它失败。
     */
    fun bleStartScan(json: ByteArray): Int {
        val ctx = activity ?: return 3
        if (!blePermissionsGranted(ctx)) return 1
        val manager = ctx.getSystemService(Context.BLUETOOTH_SERVICE) as? BluetoothManager ?: return 2
        val adapter = manager.adapter ?: return 2
        if (!adapter.isEnabled) return 2
        val scanner = adapter.bluetoothLeScanner ?: return 2
        bleScanner = scanner
        bleSeen.clear()
        synchronized(pendingBle) { pendingBle.clear() }
        val settings = ScanSettings.Builder().setScanMode(ScanSettings.SCAN_MODE_LOW_LATENCY).build()
        return try {
            scanner.startScan(null, settings, bleCallback)
            0
        } catch (e: SecurityException) {
            1
        } catch (e: Exception) {
            3
        }
    }

    /** 停止扫描；没在扫时也算成功。 */
    fun bleStopScan(): Int {
        return try {
            bleScanner?.stopScan(bleCallback)
            bleScanner = null
            0
        } catch (e: Exception) {
            3
        }
    }

    /** Go 侧轮询：取一条事件，没有就返回 null。 */
    fun takeBleEvent(): ByteArray? = synchronized(pendingBle) {
        pendingBle.removeFirstOrNull()?.toByteArray(Charsets.UTF_8)
    }

    private val bleCallback = object : ScanCallback() {
        override fun onScanResult(callbackType: Int, result: ScanResult) {
            val device = result.device ?: return
            val address = try {
                device.address
            } catch (e: SecurityException) {
                null
            } ?: return
            if (!bleSeen.add(address)) return
            // 读取设备名需要 BLUETOOTH_CONNECT；没有就退回广播里的名字。
            val name = try {
                device.name
            } catch (e: SecurityException) {
                null
            } ?: result.scanRecord?.deviceName.orEmpty()
            val event = JSONObject()
                .put("kind", "advertisement")
                .put("address", address)
                .put("name", name)
                .put("rssi", result.rssi)
            synchronized(pendingBle) {
                if (pendingBle.size < 512) {
                    pendingBle.addLast(event.toString())
                }
            }
        }

        override fun onScanFailed(errorCode: Int) {
            val event = JSONObject().put("kind", "error").put("error", "scan failed: " + errorCode)
            synchronized(pendingBle) { pendingBle.addLast(event.toString()) }
        }
    }

    /** 当前连接的 GATT 与地址；同一时刻只支持一个连接（手机控制端的常见形态）。 */
    private var bleGatt: BluetoothGatt? = null
    private var bleGattAddress: String? = null

    /**
     * 连接外设。0 = 已发起；1 = 没有权限；2 = 没有蓝牙；3 = 其它失败。
     * 连接结果与随后的服务发现都以事件回传。
     */
    fun bleConnect(json: ByteArray): Int {
        val ctx = activity ?: return 3
        if (!blePermissionsGranted(ctx)) return 1
        val address = try {
            JSONObject(String(json, Charsets.UTF_8)).optString("address")
        } catch (e: Exception) {
            ""
        }
        if (address.isEmpty()) return 3
        val manager = ctx.getSystemService(Context.BLUETOOTH_SERVICE) as? BluetoothManager ?: return 2
        val adapter = manager.adapter ?: return 2
        val device = try {
            adapter.getRemoteDevice(address)
        } catch (e: Exception) {
            return 3
        }
        bleGatt?.close()
        bleGatt = null
        bleGattAddress = address
        return try {
            bleGatt = device.connectGatt(ctx, false, bleGattCallback, BluetoothDevice.TRANSPORT_LE)
            0
        } catch (e: SecurityException) {
            1
        } catch (e: Exception) {
            3
        }
    }

    /** 断开当前连接；没有连接时也是成功。 */
    fun bleDisconnect(json: ByteArray): Int {
        return try {
            bleGatt?.disconnect()
            bleGatt?.close()
            bleGatt = null
            bleGattAddress = null
            0
        } catch (e: Exception) {
            3
        }
    }

    /** 读一个特征值；结果以 kind=value 的事件回传。 */
    fun bleRead(json: ByteArray): Int = bleWithCharacteristic(json) { gatt, c ->
        if (!gatt.readCharacteristic(c)) 3 else 0
    }

    /** 写一个特征值；json: {characteristic, value(base64), response}。 */
    fun bleWrite(json: ByteArray): Int {
        val ctx = activity ?: return 3
        if (!blePermissionsGranted(ctx)) return 1
        val gatt = bleGatt ?: return 3
        val obj = try {
            JSONObject(String(json, Charsets.UTF_8))
        } catch (e: Exception) {
            return 3
        }
        val c = bleCharacteristic(gatt, obj.optString("characteristic")) ?: return 3
        val bytes = try {
            android.util.Base64.decode(obj.optString("value"), android.util.Base64.NO_WRAP)
        } catch (e: Exception) {
            return 3
        }
        return try {
            c.writeType = if (obj.optBoolean("response", true)) {
                BluetoothGattCharacteristic.WRITE_TYPE_DEFAULT
            } else {
                BluetoothGattCharacteristic.WRITE_TYPE_NO_RESPONSE
            }
            @Suppress("DEPRECATION")
            c.value = bytes
            if (!gatt.writeCharacteristic(c)) 3 else 0
        } catch (e: SecurityException) {
            1
        } catch (e: Exception) {
            3
        }
    }

    /** 订阅/取消订阅通知；json: {characteristic, on}。 */
    fun bleSubscribe(json: ByteArray): Int {
        val ctx = activity ?: return 3
        if (!blePermissionsGranted(ctx)) return 1
        val gatt = bleGatt ?: return 3
        val obj = try {
            JSONObject(String(json, Charsets.UTF_8))
        } catch (e: Exception) {
            return 3
        }
        val c = bleCharacteristic(gatt, obj.optString("characteristic")) ?: return 3
        val on = obj.optBoolean("on", true)
        return try {
            if (!gatt.setCharacteristicNotification(c, on)) return 3
            val descriptor = c.getDescriptor(bleCCCD) ?: return 0
            descriptor.value = if (on) {
                BluetoothGattDescriptor.ENABLE_NOTIFICATION_VALUE
            } else {
                BluetoothGattDescriptor.DISABLE_NOTIFICATION_VALUE
            }
            @Suppress("DEPRECATION")
            if (!gatt.writeDescriptor(descriptor)) 3 else 0
        } catch (e: SecurityException) {
            1
        } catch (e: Exception) {
            3
        }
    }

    /**
     * 发起配对。0 = 已受理（或本来已配对，直接回一帧状态）；1 = 没有权限；
     * 2 = 没有蓝牙；3 = 其它失败。状态以 kind=bond 的事件回传：
     * 10 = 未配对（失败或解除），11 = 配对中，12 = 已配对。
     */
    fun bleCreateBond(json: ByteArray): Int {
        val ctx = activity ?: return 3
        if (!blePermissionsGranted(ctx)) return 1
        val address = try {
            JSONObject(String(json, Charsets.UTF_8)).optString("address")
        } catch (e: Exception) {
            ""
        }
        if (address.isEmpty()) return 3
        val manager = ctx.getSystemService(Context.BLUETOOTH_SERVICE) as? BluetoothManager ?: return 2
        val adapter = manager.adapter ?: return 2
        return try {
            val device = adapter.getRemoteDevice(address)
            when (device.bondState) {
                BluetoothDevice.BOND_BONDED -> {
                    // 已经配对：补一帧事件，让 Go 侧不必等到超时。
                    queueBle(JSONObject().put("kind", "bond").put("address", address).put("state", device.bondState))
                    0
                }
                BluetoothDevice.BOND_BONDING -> 0
                else -> if (device.createBond()) 0 else 3
            }
        } catch (e: SecurityException) {
            1
        } catch (e: Exception) {
            3
        }
    }

    /** 请求 MTU。0 = 已受理；1 = 没有权限；3 = 没有连接或栈拒绝。结果以 kind=mtu 回传。 */
    fun bleRequestMtu(json: ByteArray): Int {
        val ctx = activity ?: return 3
        if (!blePermissionsGranted(ctx)) return 1
        val gatt = bleGatt ?: return 3
        val mtu = try {
            JSONObject(String(json, Charsets.UTF_8)).optInt("mtu", 512)
        } catch (e: Exception) {
            512
        }
        return try {
            if (!gatt.requestMtu(mtu)) 3 else 0
        } catch (e: SecurityException) {
            1
        } catch (e: Exception) {
            3
        }
    }

    /** 配对状态变化；装上就一直在，进程结束随进程消失。 */
    private var bondReceiver: BroadcastReceiver? = null

    @SuppressLint("UnspecifiedRegisterReceiverFlag")
    private fun listenForBondState(ctx: Context) {
        if (bondReceiver != null) return
        val receiver = object : BroadcastReceiver() {
            @Suppress("DEPRECATION")
            override fun onReceive(context: Context, intent: Intent) {
                if (intent.action != BluetoothDevice.ACTION_BOND_STATE_CHANGED) return
                val device = intent.getParcelableExtra<BluetoothDevice>(BluetoothDevice.EXTRA_DEVICE) ?: return
                val state = intent.getIntExtra(BluetoothDevice.EXTRA_BOND_STATE, BluetoothDevice.BOND_NONE)
                val address = try {
                    device.address
                } catch (e: SecurityException) {
                    ""
                }
                queueBle(
                    JSONObject()
                        .put("kind", "bond")
                        .put("address", address)
                        .put("state", state),
                )
            }
        }
        val filter = IntentFilter(BluetoothDevice.ACTION_BOND_STATE_CHANGED)
        try {
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
                ctx.registerReceiver(receiver, filter, Context.RECEIVER_NOT_EXPORTED)
            } else {
                ctx.registerReceiver(receiver, filter)
            }
            bondReceiver = receiver
        } catch (e: Exception) {
            bondReceiver = null
        }
    }

    private fun bleWithCharacteristic(json: ByteArray, action: (BluetoothGatt, BluetoothGattCharacteristic) -> Int): Int {
        val ctx = activity ?: return 3
        if (!blePermissionsGranted(ctx)) return 1
        val gatt = bleGatt ?: return 3
        val obj = try {
            JSONObject(String(json, Charsets.UTF_8))
        } catch (e: Exception) {
            return 3
        }
        val c = bleCharacteristic(gatt, obj.optString("characteristic")) ?: return 3
        return try {
            action(gatt, c)
        } catch (e: SecurityException) {
            1
        } catch (e: Exception) {
            3
        }
    }

    /** "1234/5678" 用 16 位短 UUID，"<service>/<char>" 或单写特征值也接受。 */
    private fun bleCharacteristic(gatt: BluetoothGatt, spec: String): BluetoothGattCharacteristic? {
        if (spec.isEmpty()) return null
        val parts = spec.split("/", limit = 2)
        val service = if (parts.size == 2) bleUuid(parts[0]) else null
        val characteristic = bleUuid(if (parts.size == 2) parts[1] else parts[0])
        for (s in gatt.services) {
            if (service != null && s.uuid != service) continue
            for (c in s.characteristics) {
                if (c.uuid == characteristic) return c
            }
        }
        return null
    }

    private fun bleUuid(text: String): UUID {
        val trimmed = text.trim().lowercase().removePrefix("0x")
        if (trimmed.length <= 4) {
            return UUID.fromString("0000" + trimmed.padStart(4, '0') + "-0000-1000-8000-00805f9b34fb")
        }
        return UUID.fromString(trimmed)
    }

    private fun bleShortUuid(uuid: UUID): String {
        val text = uuid.toString()
        return if (text.startsWith("0000") && text.endsWith("-0000-1000-8000-00805f9b34fb")) {
            text.substring(4, 8)
        } else {
            text
        }
    }

    private fun queueBle(event: JSONObject) {
        synchronized(pendingBle) {
            if (pendingBle.size < 512) pendingBle.addLast(event.toString())
        }
    }

    private val bleGattCallback = object : BluetoothGattCallback() {
        override fun onConnectionStateChange(gatt: BluetoothGatt, status: Int, newState: Int) {
            val address = gatt.device?.address.orEmpty()
            when (newState) {
                BluetoothProfile.STATE_CONNECTED -> {
                    queueBle(JSONObject().put("kind", "connected").put("address", address))
                    try {
                        gatt.discoverServices()
                    } catch (e: Exception) {
                        queueBle(JSONObject().put("kind", "error").put("address", address).put("error", "discover: " + e))
                    }
                }
                BluetoothProfile.STATE_DISCONNECTED -> {
                    queueBle(JSONObject().put("kind", "disconnected").put("address", address).put("status", status))
                    try {
                        gatt.close()
                    } catch (e: Exception) {
                    }
                    if (bleGatt === gatt) {
                        bleGatt = null
                        bleGattAddress = null
                    }
                }
            }
        }

        override fun onServicesDiscovered(gatt: BluetoothGatt, status: Int) {
            val address = gatt.device?.address.orEmpty()
            if (status != BluetoothGatt.GATT_SUCCESS) {
                queueBle(JSONObject().put("kind", "error").put("address", address).put("error", "discover services: " + status))
                return
            }
            val services = JSONArray()
            for (s in gatt.services) {
                val chars = JSONArray()
                for (c in s.characteristics) {
                    chars.put(bleShortUuid(c.uuid))
                }
                services.put(JSONObject().put("uuid", bleShortUuid(s.uuid)).put("characteristics", chars))
            }
            queueBle(
                JSONObject()
                    .put("kind", "services")
                    .put("address", address)
                    .put("services", services),
            )
        }

        override fun onCharacteristicRead(gatt: BluetoothGatt, c: BluetoothGattCharacteristic, value: ByteArray, status: Int) {
            queueBle(
                JSONObject()
                    .put("kind", "value")
                    .put("address", gatt.device?.address.orEmpty())
                    .put("characteristic", bleShortUuid(c.uuid))
                    .put("value", android.util.Base64.encodeToString(value ?: ByteArray(0), android.util.Base64.NO_WRAP))
                    .put("status", status),
            )
        }

        override fun onCharacteristicWrite(gatt: BluetoothGatt, c: BluetoothGattCharacteristic, status: Int) {
            queueBle(
                JSONObject()
                    .put("kind", "written")
                    .put("address", gatt.device?.address.orEmpty())
                    .put("characteristic", bleShortUuid(c.uuid))
                    .put("status", status),
            )
        }

        override fun onMtuChanged(gatt: BluetoothGatt, mtu: Int, status: Int) {
            queueBle(
                JSONObject()
                    .put("kind", "mtu")
                    .put("address", gatt.device?.address.orEmpty())
                    .put("mtu", mtu)
                    .put("status", status),
            )
        }

        override fun onCharacteristicChanged(gatt: BluetoothGatt, c: BluetoothGattCharacteristic, value: ByteArray) {
            queueBle(
                JSONObject()
                    .put("kind", "value")
                    .put("address", gatt.device?.address.orEmpty())
                    .put("characteristic", bleShortUuid(c.uuid))
                    .put("value", android.util.Base64.encodeToString(value ?: ByteArray(0), android.util.Base64.NO_WRAP))
                    .put("notify", true),
            )
        }
    }

    private fun blePermissionsGranted(ctx: Context): Boolean = try {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            ctx.checkSelfPermission(android.Manifest.permission.BLUETOOTH_SCAN) == PackageManager.PERMISSION_GRANTED
        } else {
            ctx.checkSelfPermission(android.Manifest.permission.ACCESS_FINE_LOCATION) == PackageManager.PERMISSION_GRANTED
        }
    } catch (e: Exception) {
        false
    }

    // ─────────────────────────── Wi-Fi 扫描与配网 ───────────────────────────

    /** 扫描与入网事件队列：Go 侧每次操作轮询一条。 */
    private val pendingWifi = ArrayDeque<String>()

    private var wifiScanReceiver: BroadcastReceiver? = null
    private var wifiJoinCallback: ConnectivityManager.NetworkCallback? = null

    private fun queueWifi(event: JSONObject) {
        synchronized(pendingWifi) {
            if (pendingWifi.size < 256) pendingWifi.addLast(event.toString())
        }
    }

    /** Go 侧轮询：取一条 Wi-Fi 事件，没有就返回 null。 */
    fun takeWifiEvent(): ByteArray? = synchronized(pendingWifi) {
        pendingWifi.removeFirstOrNull()?.toByteArray(Charsets.UTF_8)
    }

    private fun wifiPermissionGranted(ctx: Context): Boolean = try {
        val wire = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            android.Manifest.permission.NEARBY_WIFI_DEVICES
        } else {
            android.Manifest.permission.ACCESS_FINE_LOCATION
        }
        ctx.checkSelfPermission(wire) == PackageManager.PERMISSION_GRANTED
    } catch (e: Exception) {
        false
    }

    /**
     * 扫描附近的 Wi-Fi。0 = 已发起；1 = 没有权限；2 = Wi-Fi 没开；3 = 其它失败
     * （含系统节流）。结果以 kind=networks 的事件回传。
     */
    fun wifiScan(): Int {
        val ctx = activity ?: return 3
        if (!wifiPermissionGranted(ctx)) return 1
        val manager = ctx.applicationContext.getSystemService(Context.WIFI_SERVICE) as? WifiManager ?: return 2
        if (!manager.isWifiEnabled) return 2
        synchronized(pendingWifi) { pendingWifi.clear() }
        listenForWifiResults(ctx.applicationContext)
        return try {
            @Suppress("DEPRECATION")
            if (manager.startScan()) 0 else 3
        } catch (e: SecurityException) {
            1
        } catch (e: Exception) {
            3
        }
    }

    /** 扫描结果回来时把整批网络压成一条事件，然后摘掉接收器。 */
    private fun listenForWifiResults(ctx: Context) {
        if (wifiScanReceiver != null) return
        val receiver = object : BroadcastReceiver() {
            override fun onReceive(context: Context, intent: Intent) {
                if (intent.action != WifiManager.SCAN_RESULTS_AVAILABLE_ACTION) return
                try {
                    context.unregisterReceiver(this)
                } catch (e: Exception) {
                }
                wifiScanReceiver = null
                val manager = context.applicationContext.getSystemService(Context.WIFI_SERVICE) as? WifiManager
                val results = try {
                    manager?.scanResults
                } catch (e: SecurityException) {
                    null
                } ?: return
                val networks = JSONArray()
                val seen = HashSet<String>()
                for (r in results) {
                    val ssid = try {
                        r.SSID
                    } catch (e: SecurityException) {
                        null
                    } ?: continue
                    // 隐藏网络没有 SSID；同一个 SSID 的多个热点只报信号最好的那个。
                    if (ssid.isEmpty() || !seen.add(ssid)) continue
                    networks.put(
                        JSONObject()
                            .put("ssid", ssid)
                            .put("bssid", try { r.BSSID } catch (e: SecurityException) { "" })
                            .put("rssi", r.level)
                            .put("security", securityOf(r.capabilities)),
                    )
                }
                queueWifi(JSONObject().put("kind", "networks").put("networks", networks))
            }
        }
        val filter = IntentFilter(WifiManager.SCAN_RESULTS_AVAILABLE_ACTION)
        try {
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
                ctx.registerReceiver(receiver, filter, Context.RECEIVER_NOT_EXPORTED)
            } else {
                ctx.registerReceiver(receiver, filter)
            }
            wifiScanReceiver = receiver
        } catch (e: Exception) {
            wifiScanReceiver = null
            queueWifi(JSONObject().put("kind", "error").put("error", "the scan receiver was refused"))
        }
    }

    /** Android 的 capabilities 字符串 → 一个词，够应用决定要密码还是跳浏览器。 */
    private fun securityOf(capabilities: String): String {
        val caps = capabilities.uppercase()
        return when {
            caps.contains("EAP") || caps.contains("802.1X") -> "enterprise"
            caps.contains("WPA3") || caps.contains("SAE") -> "wpa3"
            caps.contains("WPA2") || caps.contains("RSN") -> "wpa2"
            caps.contains("WPA") -> "wpa"
            caps.contains("WEP") -> "wep"
            else -> "open"
        }
    }

    /**
     * 连上一台设备自己开的热点（配网用）。0 = 已发起；1 = 没有权限；3 = 其它失败。
     * json: {"ssid","password","security"}。结果以 kind=joined / join-failed 回传，
     * 连上后**整个进程**的流量都走这台设备，直到 wifiLeave。
     */
    fun wifiJoin(json: ByteArray): Int {
        val ctx = activity ?: return 3
        if (!wifiPermissionGranted(ctx)) return 1
        val obj = try {
            JSONObject(String(json, Charsets.UTF_8))
        } catch (e: Exception) {
            return 3
        }
        val ssid = obj.optString("ssid")
        if (ssid.isEmpty()) return 3
        val password = obj.optString("password")
        val security = obj.optString("security").lowercase()
        val cm = ctx.applicationContext.getSystemService(Context.CONNECTIVITY_SERVICE) as? ConnectivityManager ?: return 3
        val specifier = try {
            val builder = WifiNetworkSpecifier.Builder().setSsid(ssid)
            if (password.isNotEmpty()) {
                if (security == "wpa3") builder.setWpa3Passphrase(password) else builder.setWpa2Passphrase(password)
            }
            builder.build()
        } catch (e: Exception) {
            return 3
        }
        // 设备热点通常没有外网：不要求 INTERNET，否则系统等验证超时才算连上。
        val request = NetworkRequest.Builder()
            .addTransportType(NetworkCapabilities.TRANSPORT_WIFI)
            .removeCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
            .setNetworkSpecifier(specifier)
            .build()
        val callback = object : ConnectivityManager.NetworkCallback() {
            override fun onAvailable(network: Network) {
                try {
                    cm.bindProcessToNetwork(network)
                } catch (e: Exception) {
                }
                queueWifi(JSONObject().put("kind", "joined").put("ssid", ssid))
            }

            override fun onUnavailable() {
                queueWifi(JSONObject().put("kind", "join-failed").put("error", "the access point did not answer"))
            }

            override fun onLost(network: Network) {
                queueWifi(JSONObject().put("kind", "lost").put("ssid", ssid))
            }
        }
        synchronized(pendingWifi) { pendingWifi.clear() }
        dropWifiRequest(cm)
        return try {
            wifiJoinCallback = callback
            cm.requestNetwork(request, callback, 30000)
            0
        } catch (e: SecurityException) {
            wifiJoinCallback = null
            1
        } catch (e: Exception) {
            wifiJoinCallback = null
            3
        }
    }

    /** 离开 wifiJoin 连上的网络，恢复默认路由；没有在配网时也算成功。 */
    fun wifiLeave(): Int {
        val ctx = activity ?: return 3
        val cm = ctx.applicationContext.getSystemService(Context.CONNECTIVITY_SERVICE) as? ConnectivityManager ?: return 3
        return try {
            dropWifiRequest(cm)
            0
        } catch (e: Exception) {
            3
        }
    }

    private fun dropWifiRequest(cm: ConnectivityManager) {
        wifiJoinCallback?.let {
            try {
                cm.unregisterNetworkCallback(it)
            } catch (e: Exception) {
            }
        }
        wifiJoinCallback = null
        try {
            cm.bindProcessToNetwork(null)
        } catch (e: Exception) {
        }
    }

    // ─────────────────────────── 音量与媒体键 ───────────────────────────

    private fun audioManager(): AudioManager? =
        activity?.applicationContext?.getSystemService(Context.AUDIO_SERVICE) as? AudioManager

    /** 媒体音量与铃声模式，一次拿全。没有 AudioManager 时返回 null。 */
    fun audioVolume(): ByteArray? {
        val am = audioManager() ?: return null
        val max = am.getStreamMaxVolume(AudioManager.STREAM_MUSIC).coerceAtLeast(1)
        val current = try {
            am.getStreamVolume(AudioManager.STREAM_MUSIC)
        } catch (e: SecurityException) {
            return null
        }
        val muted = try {
            am.isStreamMute(AudioManager.STREAM_MUSIC)
        } catch (e: Exception) {
            current == 0
        }
        val ringer = when (am.ringerMode) {
            AudioManager.RINGER_MODE_SILENT -> "silent"
            AudioManager.RINGER_MODE_VIBRATE -> "vibrate"
            else -> "normal"
        }
        return JSONObject()
            .put("percent", (current * 100 + max / 2) / max)
            .put("muted", muted)
            .put("ringer", ringer)
            .toString()
            .toByteArray(Charsets.UTF_8)
    }

    /** json 三选一：{"percent":0..100}、{"muted":bool}、{"steps":±n}。 */
    fun audioSetVolume(json: ByteArray): Int {
        val am = audioManager() ?: return 3
        val obj = try {
            JSONObject(String(json, Charsets.UTF_8))
        } catch (e: Exception) {
            return 3
        }
        return try {
            when {
                obj.has("percent") -> {
                    val max = am.getStreamMaxVolume(AudioManager.STREAM_MUSIC).coerceAtLeast(1)
                    val percent = obj.optInt("percent", 0).coerceIn(0, 100)
                    am.setStreamVolume(AudioManager.STREAM_MUSIC, Math.round(percent * max / 100f), 0)
                }
                obj.has("muted") -> am.adjustStreamVolume(
                    AudioManager.STREAM_MUSIC,
                    if (obj.optBoolean("muted")) AudioManager.ADJUST_MUTE else AudioManager.ADJUST_UNMUTE,
                    0,
                )
                obj.has("steps") -> {
                    val steps = obj.optInt("steps", 0).coerceIn(-15, 15)
                    val direction = if (steps >= 0) AudioManager.ADJUST_RAISE else AudioManager.ADJUST_LOWER
                    repeat(Math.abs(steps)) {
                        am.adjustStreamVolume(AudioManager.STREAM_MUSIC, direction, 0)
                    }
                }
                else -> return 3
            }
            0
        } catch (e: SecurityException) {
            1
        } catch (e: Exception) {
            3
        }
    }

    /** 媒体键：play-pause / play / pause / next / previous / stop / forward / rewind。 */
    fun audioMediaKey(json: ByteArray): Int {
        val am = audioManager() ?: return 3
        val name = try {
            JSONObject(String(json, Charsets.UTF_8)).optString("key")
        } catch (e: Exception) {
            ""
        }
        val code = when (name) {
            "play-pause" -> KeyEvent.KEYCODE_MEDIA_PLAY_PAUSE
            "play" -> KeyEvent.KEYCODE_MEDIA_PLAY
            "pause" -> KeyEvent.KEYCODE_MEDIA_PAUSE
            "next" -> KeyEvent.KEYCODE_MEDIA_NEXT
            "previous" -> KeyEvent.KEYCODE_MEDIA_PREVIOUS
            "stop" -> KeyEvent.KEYCODE_MEDIA_STOP
            "forward" -> KeyEvent.KEYCODE_MEDIA_FAST_FORWARD
            "rewind" -> KeyEvent.KEYCODE_MEDIA_REWIND
            else -> return 3
        }
        return try {
            am.dispatchMediaKeyEvent(KeyEvent(KeyEvent.ACTION_DOWN, code))
            am.dispatchMediaKeyEvent(KeyEvent(KeyEvent.ACTION_UP, code))
            0
        } catch (e: Exception) {
            3
        }
    }

    /** 0 = 已设置；2 = 需要「勿扰」访问权限（silent）；3 = 其它失败。 */
    fun audioRinger(json: ByteArray): Int {
        val am = audioManager() ?: return 3
        val mode = try {
            JSONObject(String(json, Charsets.UTF_8)).optString("mode")
        } catch (e: Exception) {
            ""
        }
        val target = when (mode) {
            "normal" -> AudioManager.RINGER_MODE_NORMAL
            "vibrate" -> AudioManager.RINGER_MODE_VIBRATE
            "silent" -> {
                // Android 6 起把铃声调成 silent 要「勿扰」访问权限，否则直接抛。
                if (!silenceAllowed()) return 2
                AudioManager.RINGER_MODE_SILENT
            }
            else -> return 3
        }
        return try {
            am.ringerMode = target
            0
        } catch (e: SecurityException) {
            2
        } catch (e: Exception) {
            3
        }
    }

    /** 是否已经拿到「勿扰」访问权限。 */
    fun silenceAllowed(): Boolean = try {
        val nm = activity?.applicationContext?.getSystemService(Context.NOTIFICATION_SERVICE) as? NotificationManager
        nm?.isNotificationPolicyAccessGranted ?: false
    } catch (e: Exception) {
        false
    }

    /** 打开系统的「勿扰访问」页：0 已打开；3 打不开。 */
    fun openSilenceSettings(): Int {
        val ctx = activity ?: return 3
        return try {
            ctx.startActivity(Intent(Settings.ACTION_NOTIFICATION_POLICY_ACCESS_SETTINGS))
            0
        } catch (e: Exception) {
            3
        }
    }

    // ─────────────────────────── NFC ───────────────────────────

    /** 标签事件队列：Go 侧每 120 ms 取一条。 */
    private val pendingNfc = ArrayDeque<String>()

    private var nfcAdapter: NfcAdapter? = null
    private var nfcReading = false

    /** 最后读到的标签：写标签要在同一次贴近里完成。 */
    private var nfcLastTag: Tag? = null

    private fun nfcManager(): NfcAdapter? {
        val ctx = activity ?: return null
        return (ctx.applicationContext.getSystemService(Context.NFC_SERVICE) as? NfcManager)?.defaultAdapter
    }

    /** 设备有没有 NFC 适配器（不等于已打开）。 */
    fun nfcAvailable(): Boolean = try {
        nfcManager() != null
    } catch (e: Exception) {
        false
    }

    /**
     * 开前台读卡。0 = 已开；2 = 没有 NFC 或没打开；3 = 其它失败。
     * 读到的标签以 kind=tag 的事件回传。reader mode 只在界面在前台时有效。
     */
    fun nfcScanStart(): Int {
        val act = activity ?: return 3
        val adapter = nfcManager() ?: return 2
        if (!adapter.isEnabled) return 2
        synchronized(pendingNfc) { pendingNfc.clear() }
        nfcAdapter = adapter
        val flags = NfcAdapter.FLAG_READER_NFC_A or
            NfcAdapter.FLAG_READER_NFC_B or
            NfcAdapter.FLAG_READER_NFC_F or
            NfcAdapter.FLAG_READER_NFC_V or
            NfcAdapter.FLAG_READER_NFC_BARCODE
        return try {
            adapter.enableReaderMode(act, nfcCallback, flags, null)
            nfcReading = true
            0
        } catch (e: Exception) {
            3
        }
    }

    /** 关前台读卡；没开时也算成功。 */
    fun nfcScanStop(): Int {
        val act = activity ?: return 3
        val adapter = nfcAdapter ?: return 0
        return try {
            if (nfcReading) {
                adapter.disableReaderMode(act)
                nfcReading = false
            }
            0
        } catch (e: Exception) {
            3
        }
    }

    /** Go 侧轮询：取一条标签事件，没有就返回 null。 */
    fun takeNfcEvent(): ByteArray? = synchronized(pendingNfc) {
        pendingNfc.removeFirstOrNull()?.toByteArray(Charsets.UTF_8)
    }

    private fun queueNfc(event: JSONObject) {
        synchronized(pendingNfc) {
            if (pendingNfc.size < 64) pendingNfc.addLast(event.toString())
        }
    }

    private val nfcCallback = NfcAdapter.ReaderCallback { tag ->
        nfcLastTag = tag
        queueNfc(tagJson(tag))
    }

    /** 标签 → JSON：UID、技术列表、NDEF 记录（文本/URI/MIME/其它）。 */
    private fun tagJson(tag: Tag): JSONObject {
        val id = tag.id?.joinToString("") { String.format("%02x", it) } ?: ""
        val tech = JSONArray()
        for (name in tag.techList) tech.put(name.substringAfterLast('.'))
        val event = JSONObject().put("kind", "tag").put("id", id).put("tech", tech)
        val records = JSONArray()
        var writable = false
        val ndef = Ndef.get(tag)
        if (ndef != null) {
            writable = ndef.isWritable
            try {
                val message = ndef.cachedNdefMessage ?: ndef.ndefMessage
                for (record in message?.records ?: emptyArray()) {
                    records.put(recordJson(record))
                }
            } catch (e: Exception) {
            }
        }
        return event.put("writable", writable).put("records", records)
    }

    private fun recordJson(record: NdefRecord): JSONObject {
        val obj = JSONObject()
        val payload = record.payload
        when {
            record.tnf == NdefRecord.TNF_WELL_KNOWN && record.type.contentEquals(NdefRecord.RTD_TEXT) -> {
                // 第一个字节是状态位：最高位是编码，低 6 位是语言码长度。
                val status = if (payload.isNotEmpty()) payload[0].toInt() and 0xFF else 0
                val langLen = status and 0x3F
                val encoding = if (status and 0x80 != 0) "UTF-16" else "UTF-8"
                val body = try {
                    val charset = java.nio.charset.Charset.forName(encoding)
                    String(payload, 1 + langLen, (payload.size - 1 - langLen).coerceAtLeast(0), charset)
                } catch (e: Exception) {
                    ""
                }
                val language = try {
                    String(payload, 1, langLen.coerceAtMost(payload.size - 1), Charsets.US_ASCII)
                } catch (e: Exception) {
                    ""
                }
                obj.put("type", "text").put("text", body).put("language", language)
            }
            record.tnf == NdefRecord.TNF_WELL_KNOWN && record.type.contentEquals(NdefRecord.RTD_URI) -> {
                obj.put("type", "uri").put("uri", record.toUri()?.toString() ?: "")
            }
            record.tnf == NdefRecord.TNF_MIME_MEDIA -> {
                obj.put("type", "mime").put("mime", record.toMimeType() ?: "")
            }
            record.tnf == NdefRecord.TNF_EXTERNAL_TYPE || record.tnf == NdefRecord.TNF_ABSOLUTE_URI -> {
                obj.put("type", "external").put("uri", record.toUri()?.toString() ?: "")
            }
            else -> obj.put("type", "unknown")
        }
        obj.put("raw", android.util.Base64.encodeToString(payload, android.util.Base64.NO_WRAP))
        return obj
    }

    /**
     * 往刚读到的标签写一条文本记录。0 = 已写；2 = 没有 NFC；3 = 失败（标签已离开、
     * 或没有 NDEF）；4 = 标签只读。必须在读到标签后、标签还在感应区时调用。
     */
    fun nfcWriteText(json: ByteArray): Int {
        val tag = nfcLastTag ?: return 3
        val text = try {
            JSONObject(String(json, Charsets.UTF_8)).optString("text")
        } catch (e: Exception) {
            ""
        }
        if (text.isEmpty()) return 3
        val ndef = Ndef.get(tag) ?: return 3
        return try {
            ndef.connect()
            try {
                if (!ndef.isWritable) return 4
                ndef.writeNdefMessage(NdefMessage(NdefRecord.createTextRecord("en", text)))
                0
            } finally {
                try {
                    ndef.close()
                } catch (e: Exception) {
                }
            }
        } catch (e: java.io.IOException) {
            3
        } catch (e: Exception) {
            3
        }
    }

    // ── 原生指令（非 NDEF 标签）──────────────────────────────────────

    /** 十六进制字符串 → 字节，奇数长度或非法字符返回 null。 */
    private fun hexBytes(hex: String): ByteArray? {
        val clean = hex.trim().replace(" ", "")
        if (clean.length % 2 != 0) {
            return null
        }
        return try {
            ByteArray(clean.length / 2) { i -> clean.substring(i * 2, i * 2 + 2).toInt(16).toByte() }
        } catch (e: NumberFormatException) {
            null
        }
    }

    private fun hexString(bytes: ByteArray): String = bytes.joinToString("") { String.format("%02x", it) }

    /**
     * 向感应区里的最后一张标签发一条原生命令，返回 {"data":"<hex>"}；没有标签、
     * 标签没有这种技术、或指令失败时返回 null。
     *
     * json: {"tech":"NfcA|NfcB|NfcF|NfcV|IsoDep|MifareUltralight|MifareClassic","data":"<hex>"}
     */
    fun nfcTransceive(json: ByteArray): ByteArray? {
        val tag = nfcLastTag ?: return null
        val obj = try {
            JSONObject(String(json, Charsets.UTF_8))
        } catch (e: Exception) {
            return null
        }
        val tech = obj.optString("tech", "NfcA")
        val data = hexBytes(obj.optString("data")) ?: return null
        val device: android.nfc.tech.TagTechnology = when (tech) {
            "NfcA" -> NfcA.get(tag)
            "NfcB" -> NfcB.get(tag)
            "NfcF" -> NfcF.get(tag)
            "NfcV" -> NfcV.get(tag)
            "IsoDep" -> IsoDep.get(tag)
            "MifareUltralight" -> MifareUltralight.get(tag)
            "MifareClassic" -> MifareClassic.get(tag)
            else -> null
        } ?: return null
        return try {
            device.connect()
            if (device is IsoDep) {
                device.timeout = 5000
            }
            val answer = when (device) {
                is NfcA -> device.transceive(data)
                is NfcB -> device.transceive(data)
                is NfcF -> device.transceive(data)
                is NfcV -> device.transceive(data)
                is IsoDep -> device.transceive(data)
                is MifareUltralight -> device.transceive(data)
                is MifareClassic -> device.transceive(data)
                else -> null
            } ?: return null
            JSONObject().put("data", hexString(answer)).toString().toByteArray(Charsets.UTF_8)
        } catch (e: Exception) {
            null
        } finally {
            try {
                device.close()
            } catch (e: Exception) {
            }
        }
    }

    // ── 卡模拟（HCE）─────────────────────────────────────────────────

    private class HceRequest(val id: Long, val apdu: ByteArray)

    private val hceRequests = ArrayDeque<HceRequest>()
    private val hceWaiters = java.util.concurrent.ConcurrentHashMap<Long, java.util.concurrent.ArrayBlockingQueue<ByteArray>>()
    private val hceIds = java.util.concurrent.atomic.AtomicLong()

    @Volatile
    private var hceRunning = false

    @Volatile
    private var hceTimeoutMs = 5000L

    /** 没有应答时的状态字：6F00（没有精确诊断）。 */
    private val hceUnavailable = byteArrayOf(0x6F, 0x00)

    /**
     * 开启卡模拟。0 = 已开；2 = 没有 NFC 或没打开；3 = 失败。
     * json: {"aids":["F04D59474F3031"],"timeout":5000}
     *
     * 只在应用位于前台时可靠：会把自己设成首选服务。
     */
    fun hceStart(json: ByteArray): Int {
        val ctx = activity ?: return 3
        val adapter = nfcManager() ?: return 2
        if (!adapter.isEnabled) return 2
        val obj = try {
            JSONObject(String(json, Charsets.UTF_8))
        } catch (e: Exception) {
            return 3
        }
        val aids = mutableListOf<String>()
        obj.optJSONArray("aids")?.let { array ->
            for (i in 0 until array.length()) {
                val aid = array.optString(i).trim()
                if (aid.isNotEmpty()) aids.add(aid)
            }
        }
        val timeout = obj.optLong("timeout", 5000L)
        val component = ComponentName(ctx, MygoHostApduService::class.java)
        return try {
            val emulation = CardEmulation.getInstance(adapter)
            if (aids.isNotEmpty()) {
                val registered = emulation.registerAidsForService(component, CardEmulation.CATEGORY_OTHER, aids)
                if (!registered) return 3
            }
            synchronized(hceRequests) {
                hceRequests.clear()
                hceTimeoutMs = timeout.coerceIn(200L, 30_000L)
                hceRunning = true
            }
            try {
                emulation.setPreferredService(activity, component)
            } catch (e: Exception) {
                // 不在前台时系统会拒绝，路由仍按 AID 走。
            }
            0
        } catch (e: Exception) {
            3
        }
    }

    /** 关闭卡模拟：撤销 AID、取消首选，并唤醒所有还在等应答的调用。 */
    fun hceStop(): Int {
        val ctx = activity ?: return 3
        synchronized(hceRequests) {
            hceRunning = false
            hceRequests.clear()
        }
        hceWaiters.values.forEach { it.offer(ByteArray(0)) }
        val adapter = nfcManager() ?: return 0
        val component = ComponentName(ctx, MygoHostApduService::class.java)
        return try {
            val emulation = CardEmulation.getInstance(adapter)
            try {
                activity?.let { emulation.unsetPreferredService(it) }
            } catch (e: Exception) {
            }
            try {
                emulation.removeAidsForService(component, CardEmulation.CATEGORY_OTHER)
            } catch (e: Exception) {
            }
            0
        } catch (e: Exception) {
            3
        }
    }

    /** Go 侧轮询：取一条待处理的 APDU，{"id":n,"apdu":"<hex>"}，没有就返回 null。 */
    fun takeHceRequest(): ByteArray? {
        val request = synchronized(hceRequests) { hceRequests.removeFirstOrNull() } ?: return null
        return JSONObject()
            .put("id", request.id)
            .put("apdu", hexString(request.apdu))
            .toString()
            .toByteArray(Charsets.UTF_8)
    }

    /** Go 侧应答：{"id":n,"data":"<hex>"}；data 为空表示给不出应答（回 6F00）。 */
    fun hceRespond(json: ByteArray): Int {
        val obj = try {
            JSONObject(String(json, Charsets.UTF_8))
        } catch (e: Exception) {
            return 3
        }
        val waiter = hceWaiters[obj.optLong("id", -1)] ?: return 3
        waiter.offer(hexBytes(obj.optString("data")) ?: ByteArray(0))
        return 0
    }

    /**
     * 服务的入口：把 APDU 交给 Go 侧并等应答。Reader 侧同步调用，最长等
     * hceTimeoutMs；超时或没有开启卡模拟时回 6F00。
     */
    fun hceApdu(apdu: ByteArray): ByteArray {
        val waiter = java.util.concurrent.ArrayBlockingQueue<ByteArray>(1)
        val id = hceIds.incrementAndGet()
        synchronized(hceRequests) {
            if (!hceRunning) return hceUnavailable
            hceWaiters[id] = waiter
            if (hceRequests.size >= 32) {
                val dropped = hceRequests.removeFirst()
                hceWaiters.remove(dropped.id)?.offer(ByteArray(0))
            }
            hceRequests.addLast(HceRequest(id, apdu))
        }
        val answer = try {
            waiter.poll(hceTimeoutMs, java.util.concurrent.TimeUnit.MILLISECONDS)
        } catch (e: InterruptedException) {
            null
        }
        hceWaiters.remove(id)
        return if (answer == null || answer.isEmpty()) hceUnavailable else answer
    }

    /** 服务被系统切走：放弃还在等待的请求。 */
    fun hceDeactivated(reason: Int) {
        synchronized(hceRequests) {
            hceRequests.clear()
        }
        hceWaiters.values.forEach { it.offer(ByteArray(0)) }
    }

    // ─────────────────────────── 语音合成 ───────────────────────────

    /** 0 未启动 / 1 初始化中 / 2 就绪 / 3 失败。 */
    @Volatile
    private var ttsState = 0
    private var tts: TextToSpeech? = null

    /**
     * 用系统语音合成朗读。json: {"text":…, "rate":…, "pitch":…, "flush":…}。
     * 返回 0 = 已交给引擎；1 = 引擎正在初始化，稍后重试；2 = 设备没有可用引擎；
     * 3 = 其它失败。
     */
    fun speak(json: ByteArray): Int {
        val ctx = activity ?: return 3
        val obj = try {
            JSONObject(String(json, Charsets.UTF_8))
        } catch (e: Exception) {
            return 3
        }
        ensureTTS(ctx)
        when (ttsState) {
            2 -> {}
            3 -> return 2
            else -> return 1
        }
        val text = obj.optString("text")
        if (text.isEmpty()) return 3
        val engine = tts ?: return 2
        engine.setSpeechRate(obj.optDouble("rate", 1.0).toFloat().coerceIn(0.1f, 3f))
        engine.setPitch(obj.optDouble("pitch", 1.0).toFloat().coerceIn(0.5f, 2f))
        val mode = if (obj.optBoolean("queue", false)) TextToSpeech.QUEUE_ADD else TextToSpeech.QUEUE_FLUSH
        return if (engine.speak(text, mode, null, "mygo") == TextToSpeech.SUCCESS) 0 else 3
    }

    /** 停止朗读；引擎没起来时也算成功。 */
    fun stopSpeaking(): Int {
        if (ttsState == 2) {
            tts?.stop()
        }
        return 0
    }

    private fun ensureTTS(ctx: Context) {
        if (ttsState != 0) return
        ttsState = 1
        try {
            tts = TextToSpeech(ctx.applicationContext) { status ->
                ttsState = if (status == TextToSpeech.SUCCESS) 2 else 3
            }
        } catch (e: Exception) {
            ttsState = 3
        }
    }

    /**
     * 快捷设置磁贴的内容：{"label":…, "subtitle":…, "active":…}。没有添加磁贴
     * 时也返回 0——内容先存着，添加后 onStartListening 会取。
     */
    fun updateTile(json: ByteArray): Int {
        val ctx = activity ?: return 2
        tileJSON = String(json, Charsets.UTF_8)
        return try {
            TileService.requestListeningState(ctx, ComponentName(ctx, MygoTileService::class.java))
            0
        } catch (e: Exception) {
            2
        }
    }

    /** 磁贴服务读它；解析失败当作没设置过。 */
    fun tileContent(): JSONObject? = try {
        tileJSON?.takeIf { it.isNotEmpty() }?.let { JSONObject(it) }
    } catch (e: Exception) {
        null
    }

    /** 磁贴被点了：交给 Go 侧。 */
    fun tileClicked() {
        nativeTileClick()
    }

    /** 磁贴点击：无参数。 */
    private external fun nativeTileClick()

    /**
     * 系统为这个应用配置的 HTTP 代理（全局设置或当前网络的代理）。直连时返回
     * 空对象。JSON：{"host":…, "port":…}。Go 的 socket 不会自动走它。
     */
    fun systemProxy(): ByteArray {
        return try {
            val selector = java.net.ProxySelector.getDefault()
                ?: return "{}".toByteArray(Charsets.UTF_8)
            val proxy = selector.select(java.net.URI("https://example.com"))?.firstOrNull()
                ?: return "{}".toByteArray(Charsets.UTF_8)
            if (proxy.type() != java.net.Proxy.Type.HTTP) return "{}".toByteArray(Charsets.UTF_8)
            val addr = proxy.address() as? java.net.InetSocketAddress
                ?: return "{}".toByteArray(Charsets.UTF_8)
            JSONObject()
                .put("host", addr.hostString)
                .put("port", addr.port)
                .toString().toByteArray(Charsets.UTF_8)
        } catch (e: Exception) {
            "{}".toByteArray(Charsets.UTF_8)
        }
    }

    /**
     * 截屏保护：开启后窗口内容不会出现在截屏、录屏和最近任务缩略图里。
     * 返回 0 = 已应用；1 = 没有 Activity。
     */
    fun setSecureScreen(on: Boolean): Int {
        val ctx = activity ?: return 1
        onUi {
            if (on) {
                ctx.window.addFlags(WindowManager.LayoutParams.FLAG_SECURE)
            } else {
                ctx.window.clearFlags(WindowManager.LayoutParams.FLAG_SECURE)
            }
        }
        return 0
    }

    /**
     * 是否已被系统豁免电池优化（「不受限制」）。0 = 已豁免；1 = 未豁免；
     * 2 = 没有 Activity。
     */
    fun batteryExempt(): Int {
        val ctx = activity ?: return 2
        val pm = ctx.getSystemService(Context.POWER_SERVICE) as? PowerManager ?: return 2
        return if (pm.isIgnoringBatteryOptimizations(ctx.packageName)) 0 else 1
    }

    /**
     * 请系统弹「允许后台运行」的对话框。0 = 弹了对话框；1 = 部分 ROM 不接受
     * 直接请求，退到设置里的电池优化列表；2 = 失败。
     */
    fun requestBatteryExemption(): Int {
        val ctx = activity ?: return 2
        return try {
            try {
                ctx.startActivity(
                    Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS)
                        .setData(Uri.parse("package:" + ctx.packageName))
                        .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK),
                )
                return 0
            } catch (e: ActivityNotFoundException) {
                // 落到下面的列表页
            }
            ctx.startActivity(
                Intent(Settings.ACTION_IGNORE_BATTERY_OPTIMIZATION_SETTINGS)
                    .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK),
            )
            1
        } catch (e: Exception) {
            2
        }
    }

    /** 组播锁；同一时刻只持有一个，参考计数关掉，自己管生命周期。 */
    private var multicastLock: WifiManager.MulticastLock? = null

    /**
     * Wi-Fi 组播锁：没有它，系统不会把 mDNS/SSDP 的组播包投给这个应用，
     * 任何发现库都收不到东西。返回 0 = 已切换；1 = 设备没有 Wi-Fi；
     * 2 = 没有 Activity 或调用失败。
     */
    fun setMulticast(on: Boolean): Int {
        val ctx = activity ?: return 2
        val wm = ctx.applicationContext.getSystemService(Context.WIFI_SERVICE) as? WifiManager ?: return 1
        return try {
            if (on) {
                if (multicastLock?.isHeld != true) {
                    multicastLock = wm.createMulticastLock("mygo").apply {
                        setReferenceCounted(false)
                        acquire()
                    }
                }
            } else {
                multicastLock?.let { if (it.isHeld) it.release() }
                multicastLock = null
            }
            0
        } catch (e: Exception) {
            2
        }
    }

    /**
     * 前台服务：reason 显示在常驻通知里，进程切后台不被回收。
     * 返回 0 = 已启动；1 = 没有 Activity；2 = 启动失败（Android 12+ 从后台
     * 启动前台服务会被拒）。
     */
    @Synchronized
    fun startForeground(reason: ByteArray): Int {
        // 只需要 Context 起服务，不需要界面：没有 Activity 的宿主（游戏引擎）
        // 用 Application Context 也能起前台服务。
        val ctx = activity ?: appContext ?: return 1
        return try {
            val intent = Intent(ctx, MygoForegroundService::class.java)
                .putExtra(EXTRA_FOREGROUND_REASON, String(reason, Charsets.UTF_8))
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                ctx.startForegroundService(intent)
            } else {
                ctx.startService(intent)
            }
            foregroundHolders++
            0
        } catch (e: Exception) {
            2
        }
    }

    /**
     * 释放一次持有；最后一个 release 才真的停服务。没有持有者时也是成功。
     */
    @Synchronized
    fun stopForeground(): Int {
        val ctx = activity ?: appContext ?: return 1
        foregroundHolders = maxOf(0, foregroundHolders - 1)
        if (foregroundHolders > 0) {
            return 0
        }
        return try {
            ctx.stopService(Intent(ctx, MygoForegroundService::class.java))
            0
        } catch (e: Exception) {
            2
        }
    }

    /** 小组件当前展示的内容，JSON；null 表示还没设置过。 */
    @Volatile
    private var widgetJson: String? = null

    /**
     * 小组件内容。Go 侧推一份 JSON：{"title":…, "rows":[{"id":…,"label":…,"subtitle":…}]}，
     * 最多 4 行；未使用的行隐藏。桌面上没有实例时也返回 0（只是没人显示）。
     */
    fun updateWidget(json: ByteArray): Int {
        val ctx = activity ?: return 2
        widgetJson = String(json, Charsets.UTF_8)
        return refreshWidgets(ctx)
    }

    /** 内容变了以后，把所有实例都刷新一遍。 */
    @Synchronized
    private fun refreshWidgets(ctx: Context): Int {
        return try {
            val manager = AppWidgetManager.getInstance(ctx)
            val views = buildWidgetViews(ctx)
            for (id in manager.getAppWidgetIds(ComponentName(ctx, MygoWidgetProvider::class.java))) {
                manager.updateAppWidget(id, views)
            }
            0
        } catch (e: Exception) {
            2
        }
    }

    /**
     * 按缓存的内容构建 RemoteViews。系统在 onUpdate 里也会调它，所以没有缓存时
     * 显示一行「打开应用加载」而不是空白。
     */
    fun buildWidgetViews(ctx: Context): RemoteViews {
        val views = RemoteViews(ctx.packageName, R.layout.mygo_widget)
        val obj = try {
            widgetJson?.takeIf { it.isNotEmpty() }?.let { JSONObject(it) }
        } catch (e: Exception) {
            null
        }
        val title = obj?.optString("title").orEmpty()
        views.setTextViewText(R.id.mygo_widget_title, title.ifEmpty { "MyGo" })
        val rows = obj?.optJSONArray("rows")
        val ids = intArrayOf(
            R.id.mygo_widget_row0, R.id.mygo_widget_row1, R.id.mygo_widget_row2, R.id.mygo_widget_row3,
        )
        val labels = intArrayOf(
            R.id.mygo_widget_row0_label, R.id.mygo_widget_row1_label,
            R.id.mygo_widget_row2_label, R.id.mygo_widget_row3_label,
        )
        val subs = intArrayOf(
            R.id.mygo_widget_row0_sub, R.id.mygo_widget_row1_sub,
            R.id.mygo_widget_row2_sub, R.id.mygo_widget_row3_sub,
        )
        for (i in 0 until 4) {
            val row = rows?.optJSONObject(i)
            if (row == null) {
                views.setViewVisibility(ids[i], View.GONE)
                continue
            }
            views.setViewVisibility(ids[i], View.VISIBLE)
            views.setTextViewText(labels[i], row.optString("label"))
            views.setTextViewText(subs[i], row.optString("subtitle"))
            val rowID = row.optString("id")
            if (rowID.isNotEmpty()) {
                views.setOnClickPendingIntent(ids[i], widgetRowIntent(ctx, i, rowID))
            }
        }
        return views
    }

    private fun widgetRowIntent(ctx: Context, index: Int, id: String): PendingIntent {
        val intent = Intent(ctx, MygoWidgetProvider::class.java)
            .setAction(MygoWidgetProvider.ACTION_ROW)
            .setData(Uri.parse("mygo://widget/" + Uri.encode(id)))
            .putExtra(MygoWidgetProvider.EXTRA_ROW, id)
        return PendingIntent.getBroadcast(
            ctx,
            index,
            intent,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
    }

    /** 小组件里点了一行：把行的 id 交给 Go 侧。 */
    fun widgetAction(id: String) {
        nativeWidgetAction(id.toByteArray(Charsets.UTF_8))
    }

    /**
     * 最后一次定位（GPS/网络/被动都可能），**不主动发起定位**，所以可能是旧值，
     * time 字段说明是什么时候的。没有权限或没有任何 provider 给过位置时返回空
     * 对象。JSON：{"lat":…, "lon":…, "accuracy":…, "time":毫秒}。
     */
    fun lastLocation(): ByteArray {
        val ctx = activity ?: return "{}".toByteArray(Charsets.UTF_8)
        val fine = ctx.checkSelfPermission(android.Manifest.permission.ACCESS_FINE_LOCATION) ==
            PackageManager.PERMISSION_GRANTED
        val coarse = ctx.checkSelfPermission(android.Manifest.permission.ACCESS_COARSE_LOCATION) ==
            PackageManager.PERMISSION_GRANTED
        if (!fine && !coarse) return "{}".toByteArray(Charsets.UTF_8)
        val lm = ctx.getSystemService(Context.LOCATION_SERVICE) as? LocationManager
            ?: return "{}".toByteArray(Charsets.UTF_8)
        var best: Location? = null
        for (provider in lm.getProviders(true)) {
            val fix = try {
                lm.getLastKnownLocation(provider)
            } catch (e: SecurityException) {
                null
            } ?: continue
            val current = best
            if (current == null || fix.time > current.time) {
                best = fix
            }
        }
        val fix = best ?: return "{}".toByteArray(Charsets.UTF_8)
        return JSONObject()
            .put("lat", fix.latitude)
            .put("lon", fix.longitude)
            .put("accuracy", fix.accuracy.toDouble())
            .put("time", fix.time)
            .toString().toByteArray(Charsets.UTF_8)
    }

    /**
     * 当前 Wi-Fi 的名字。SSID/BSSID 需要定位或 NEARBY_WIFI_DEVICES 权限，
     * 系统不给时返回空串而不是报错 —— 与「没有 Wi-Fi」在调用方看来一样。
     */
    fun wifiInfo(): ByteArray {
        val ctx = activity ?: return "{}".toByteArray(Charsets.UTF_8)
        val wm = ctx.applicationContext.getSystemService(Context.WIFI_SERVICE) as? WifiManager
            ?: return "{}".toByteArray(Charsets.UTF_8)
        val info = try {
            wm.connectionInfo
        } catch (e: Exception) {
            null
        } ?: return "{}".toByteArray(Charsets.UTF_8)
        val ssid = info.ssid?.trim('"').orEmpty().let { if (it == "<unknown ssid>") "" else it }
        val bssid = info.bssid.orEmpty().let { if (it == "02:00:00:00:00:00") "" else it }
        val obj = JSONObject()
            .put("ssid", ssid)
            .put("bssid", bssid)
            .put("connected", info.networkId != -1)
        return obj.toString().toByteArray(Charsets.UTF_8)
    }

    fun haptic(kind: Int): Int {
        val ctx = activity ?: return 2
        val v = ctx.getSystemService(Context.VIBRATOR_SERVICE) as? Vibrator ?: return 1
        if (!v.hasVibrator()) return 1
        return try {
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                v.vibrate(effectFor(kind))
            } else {
                @Suppress("DEPRECATION")
                v.vibrate(durationFor(kind))
            }
            0
        } catch (e: RuntimeException) {
            2
        }
    }

    private fun effectFor(kind: Int): VibrationEffect = when (kind) {
        0 -> VibrationEffect.createOneShot(10, 60)
        1 -> VibrationEffect.createOneShot(15, 110)
        2 -> VibrationEffect.createOneShot(25, 170)
        3 -> VibrationEffect.createOneShot(40, 255)
        4 -> VibrationEffect.createOneShot(20, 80)
        5 -> VibrationEffect.createOneShot(15, 220)
        6 -> VibrationEffect.createWaveform(longArrayOf(0, 12, 60, 12), intArrayOf(0, 180, 0, 120), -1)
        7 -> VibrationEffect.createWaveform(longArrayOf(0, 30, 70, 30), intArrayOf(0, 210, 0, 160), -1)
        else -> VibrationEffect.createWaveform(
            longArrayOf(0, 40, 50, 40, 50, 40),
            intArrayOf(0, 255, 0, 255, 0, 255),
            -1,
        )
    }

    private fun durationFor(kind: Int): Long = when (kind) {
        3 -> 40L
        6, 7 -> 60L
        8 -> 120L
        else -> 20L
    }

    // ─────────────────────────── 推送 ───────────────────────────

    /**
     * 启动 FCM 注册。token 或错误经 pendingMedia（kind 2/3）回到 Go，
     * 由 PushRegistered 交给 OnPushToken / OnPushRegistrationError。
     * 返回 0 = 已开始；2 = 没有 Activity。
     */
    fun registerPush(): Int {
        val ctx = activity ?: return 2
        return try {
            ensureFirebase(ctx)
            val main = Handler(Looper.getMainLooper())
            var answered = false
            val timeout = Runnable {
                if (!answered) {
                    answered = true
                    deliverMedia(3, 0, "token request timed out (is Google Play services available?)")
                }
            }
            main.postDelayed(timeout, 10_000)
            FirebaseMessaging.getInstance().token.addOnCompleteListener { task ->
                if (answered) return@addOnCompleteListener
                answered = true
                main.removeCallbacks(timeout)
                if (task.isSuccessful) {
                    deliverMedia(2, 0, task.result ?: "")
                } else {
                    deliverMedia(3, 0, task.exception?.message ?: "token request failed")
                }
            }
            0
        } catch (e: Exception) {
            deliverMedia(3, 0, e.message ?: e.toString())
            2
        }
    }

    /** FirebaseMessagingService 的 token 刷新转进来。 */
    fun deliverPushToken(token: String) { deliverMedia(2, 0, token) }

    /**
     * 用 assets/google-services.json 手动初始化 Firebase：本工程不接
     * google-services Gradle 插件（AGP 9 的 newDsl 与它的兼容性不稳），
     * 所以这里读同一份配置构造 FirebaseOptions。
     */
    private fun ensureFirebase(ctx: Context) {
        if (FirebaseApp.getApps(ctx).isNotEmpty()) return
        val json = ctx.assets.open("google-services.json").bufferedReader(Charsets.UTF_8).use { it.readText() }
        val root = JSONObject(json)
        val client = root.getJSONArray("client").getJSONObject(0)
        val project = root.getJSONObject("project_info")
        val opts = FirebaseOptions.Builder()
            .setApplicationId(client.getJSONObject("client_info").getString("mobilesdk_app_id"))
            .setApiKey(client.getJSONArray("api_key").getJSONObject(0).getString("current_key"))
            .setProjectId(project.getString("project_id"))
            .setGcmSenderId(project.getString("project_number"))
            .build()
        FirebaseApp.initializeApp(ctx, opts)
    }

    // ─────────────────────────── 选图 / 导出 ───────────────────────────

    private val pendingMedia = ArrayDeque<String>()

    @Synchronized
    private fun deliverMedia(kind: Int, token: Long, payload: String) {
        pendingMedia.addLast("$kind\n$token\n$payload")
    }

    /** Go 侧轮询调用：取走一条待发的选图/导出结果，没有就返回 null。 */
    @Synchronized
    fun takeMediaResult(): String? = if (pendingMedia.isEmpty()) null else pendingMedia.removeFirst()

    // ─────────────────────────── 权限 ───────────────────────────

    private const val KIND_CAMERA = 0
    private const val KIND_MICROPHONE = 1 // microphone 与 speech 是同一个 RECORD_AUDIO
    private const val KIND_NOTIFICATIONS = 2
    private const val KIND_WIFI = 3
    private const val KIND_LOCATION = 4
    private const val KIND_BLUETOOTH = 5

    private val pendingPermissions = ArrayDeque<String>()
    private val askedPermissions = java.util.concurrent.ConcurrentHashMap.newKeySet<Int>()

    @Volatile
    private var permissionPromptUp = false

    @Volatile
    private var pendingPermissionToken: Long = 0

    /**
     * 授权状态：0 已授予、1 已拒绝、2 尚未询问、3 不支持。
     *
     * Android 的 checkSelfPermission 只有 granted / denied 两档，**没有** "尚未询问"：
     * 这里用"本进程还没问过 + 系统不给 rationale"近似它。进程重启后，永久拒绝会重新读成 2，
     * 代价是多弹一次（系统会立刻回 denied，见 requestPermission）。
     */
    fun permissionStatus(kind: Int): Int {
        val ctx = activity ?: return 3
        if (kind == KIND_NOTIFICATIONS) {
            if (notificationsAllowed()) return 0
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU && kind !in askedPermissions) return 2
            return 1
        }
        val wire = androidPermission(kind) ?: return 3
        if (ctx.checkSelfPermission(wire) == PackageManager.PERMISSION_GRANTED) return 0
        if (kind !in askedPermissions && !ctx.shouldShowRequestPermissionRationale(wire)) return 2
        return 1
    }

    /**
     * 申请权限。返回 0 = 已发起（结果经 takePermissionResult 回到 Go）；
     * 3 = 现在开不了对话框（没有 Activity、另一个对话框还在等、权限不存在）。
     *
     * 同一时刻只允许一个对话框：系统的授权框本来就是串行的，第二个请求在这里被挡掉，
     * 而不是覆盖第一个的 token。
     */
    fun requestPermission(kind: Int, token: Long): Int {
        val ctx = activity ?: return 3
        if (kind == KIND_NOTIFICATIONS && Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU) {
            // 13 以下没有这个运行时权限：设置里关掉通知也弹不出授权框，直接回答现状。
            deliverPermission(token, if (notificationsAllowed()) 0 else 1)
            return 0
        }
        val wire = if (kind == KIND_NOTIFICATIONS) {
            Manifest_permission_POST_NOTIFICATIONS()
        } else {
            androidPermission(kind) ?: return 3
        }
        if (ctx.checkSelfPermission(wire) == PackageManager.PERMISSION_GRANTED) {
            deliverPermission(token, 0)
            return 0
        }
        if (permissionPromptUp) return 3
        permissionPromptUp = true
        pendingPermissionToken = token
        askedPermissions.add(kind)
        onUi {
            try {
                ctx.requestPermissions(arrayOf(wire), PERMISSION_REQUEST)
            } catch (e: Exception) {
                permissionPromptUp = false
                deliverPermission(token, 3)
            }
        }
        return 0
    }

    /** 打开本应用的系统设置页；0 = 已打开，1 = 打不开，3 = 没有 Activity。 */
    fun openAppSettings(): Int {
        val ctx = activity ?: return 3
        return try {
            val intent = Intent(
                android.provider.Settings.ACTION_APPLICATION_DETAILS_SETTINGS,
                Uri.fromParts("package", ctx.packageName, null),
            )
            ctx.startActivity(intent)
            0
        } catch (e: Exception) {
            1
        }
    }

    /** MygoActivity.onRequestPermissionsResult 转进来。 */
    fun onPermissionResult(requestCode: Int, grantResults: IntArray) {
        if (requestCode != PERMISSION_REQUEST || !permissionPromptUp) return
        permissionPromptUp = false
        val token = pendingPermissionToken
        pendingPermissionToken = 0
        val granted = grantResults.isNotEmpty() && grantResults[0] == PackageManager.PERMISSION_GRANTED
        if (granted) deliverPermission(token, 0) else deliverPermission(token, 1)
    }

    @Synchronized
    private fun deliverPermission(token: Long, status: Int) {
        pendingPermissions.addLast("$token\n$status")
    }

    /** Go 侧轮询调用：取走一条权限结果，没有就返回 null。 */
    @Synchronized
    fun takePermissionResult(): String? = if (pendingPermissions.isEmpty()) null else pendingPermissions.removeFirst()

    private fun androidPermission(kind: Int): String? = when (kind) {
        KIND_CAMERA -> "android.permission.CAMERA"
        KIND_MICROPHONE -> "android.permission.RECORD_AUDIO"
        // 33+ 用 NEARBY_WIFI_DEVICES；更老的系统上 Wi-Fi 信息仍由定位权限把守。
        KIND_WIFI -> if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            "android.permission.NEARBY_WIFI_DEVICES"
        } else {
            "android.permission.ACCESS_FINE_LOCATION"
        }
        KIND_LOCATION -> "android.permission.ACCESS_FINE_LOCATION"
        // 31+ 的蓝牙扫描权限；更老的系统上扫描仍由定位权限把守。
        KIND_BLUETOOTH -> if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            "android.permission.BLUETOOTH_SCAN"
        } else {
            "android.permission.ACCESS_FINE_LOCATION"
        }
        else -> null
    }

    // ─────────────────────────── 设备信息 ───────────────────────────

    /**
     * 手机的身份，给设置页和已配对桌面看：用户起的设备名（系统设置里的"设备名称"）
     * 或厂商 + 型号，加上 Android 版本。用 UTF-8 JSON 回传：设备名可以带任何字符，
     * 包括 BMP 之外的 emoji，JNI 的 String 会拆坏它们。
     */
    fun deviceInfo(): ByteArray {
        val ctx = activity ?: return ByteArray(0)
        val model = Build.MODEL ?: ""
        val configured = try {
            android.provider.Settings.Global.getString(ctx.contentResolver, "device_name")
        } catch (e: Exception) {
            null
        }
        val name = configured?.takeIf { it.isNotBlank() }
            ?: listOf(Build.MANUFACTURER, model).filter { !it.isNullOrBlank() }.joinToString(" ")
        val info = JSONObject()
        info.put("name", name)
        info.put("system", "Android")
        info.put("version", Build.VERSION.RELEASE ?: "")
        info.put("model", model)
        return info.toString().toByteArray(Charsets.UTF_8)
    }

    // ─────────────────────────── 语音识别 ───────────────────────────

    private val speechResults = ArrayDeque<String>()
    private var recognizer: android.speech.SpeechRecognizer? = null

    /**
     * 开始识别。返回 0 = 已开始（结果经 takeSpeechResult 回到 Go 侧面）；
     * 1 = 设备没有语音服务；2 = 没有 Activity / 启动失败。
     *
     * 识别器只能在主线程创建和启动，所以调用一律 post 到主线程；失败通过结果队列回传，
     * 而不是让这里同步返回——调用方在 Go 的 pump 线程上，等不了主线程。
     */
    fun speechStart(token: Long, language: ByteArray?, onDevice: Boolean, contextual: ByteArray?): Int {
        val ctx = activity ?: return 2
        if (!android.speech.SpeechRecognizer.isRecognitionAvailable(ctx)) return 1
        val lang = language?.toString(Charsets.UTF_8) ?: ""
        val hints = contextual?.toString(Charsets.UTF_8)
            ?.split("\n")?.filter { it.isNotEmpty() } ?: emptyList()
        onUi {
            try {
                releaseRecognizer()
                val r = android.speech.SpeechRecognizer.createSpeechRecognizer(ctx)
                r.setRecognitionListener(speechListener(token))
                val intent = Intent(android.speech.RecognizerIntent.ACTION_RECOGNIZE_SPEECH).apply {
                    putExtra(
                        android.speech.RecognizerIntent.EXTRA_LANGUAGE_MODEL,
                        android.speech.RecognizerIntent.LANGUAGE_MODEL_FREE_FORM,
                    )
                    // 部分结果就是"边说边出字"：MyGo 用它们替换整段草稿。
                    putExtra(android.speech.RecognizerIntent.EXTRA_PARTIAL_RESULTS, true)
                    if (lang.isNotEmpty()) putExtra(android.speech.RecognizerIntent.EXTRA_LANGUAGE, lang)
                    if (onDevice) putExtra(android.speech.RecognizerIntent.EXTRA_PREFER_OFFLINE, true)
                    if (hints.isNotEmpty()) putStringArrayListExtra(
                        android.speech.RecognizerIntent.EXTRA_BIASING_STRINGS,
                        ArrayList(hints),
                    )
                }
                r.startListening(intent)
                recognizer = r
            } catch (e: Exception) {
                deliverSpeech(token, 2, e.message ?: e.toString())
            }
        }
        return 0
    }

    /** 停下并等最终结果。 */
    fun speechStop(token: Long): Boolean {
        onUi {
            try {
                recognizer?.stopListening()
            } catch (e: Exception) {
                deliverSpeech(token, 2, e.message ?: e.toString())
            }
        }
        return true
    }

    /** 取消：销毁识别器，之后不再有结果。 */
    fun speechCancel(token: Long): Boolean {
        onUi { releaseRecognizer() }
        return true
    }

    /** Go 侧轮询调用：取走一条识别结果，没有就返回 null。 */
    @Synchronized
    fun takeSpeechResult(): String? = if (speechResults.isEmpty()) null else speechResults.removeFirst()

    @Synchronized
    private fun deliverSpeech(token: Long, code: Int, text: String) {
        speechResults.addLast("$token\n$code\n$text")
    }

    private fun speechListener(token: Long) = object : android.speech.RecognitionListener {
        override fun onPartialResults(partialResults: Bundle?) {
            deliverSpeech(token, 0, speechText(partialResults))
        }

        override fun onResults(results: Bundle?) {
            deliverSpeech(token, 1, speechText(results))
        }

        override fun onError(error: Int) {
            when (error) {
                // 没听到、静默太久：正常结束，空结果，不是失败。
                android.speech.SpeechRecognizer.ERROR_NO_MATCH,
                android.speech.SpeechRecognizer.ERROR_SPEECH_TIMEOUT,
                -> deliverSpeech(token, 1, "")

                android.speech.SpeechRecognizer.ERROR_INSUFFICIENT_PERMISSIONS ->
                    deliverSpeech(token, 2, "permission_denied")

                else -> deliverSpeech(token, 2, speechErrorName(error))
            }
            onUi { releaseRecognizer() }
        }

        override fun onReadyForSpeech(params: Bundle?) {}
        override fun onBeginningOfSpeech() {}
        override fun onRmsChanged(rmsdB: Float) {}
        override fun onBufferReceived(buffer: ByteArray?) {}
        override fun onEndOfSpeech() {}
        override fun onEvent(eventType: Int, params: Bundle?) {}
    }

    private fun speechText(bundle: Bundle?): String =
        bundle?.getStringArrayList(android.speech.SpeechRecognizer.RESULTS_RECOGNITION)?.firstOrNull() ?: ""

    private fun speechErrorName(error: Int): String = when (error) {
        android.speech.SpeechRecognizer.ERROR_AUDIO -> "audio_error"
        android.speech.SpeechRecognizer.ERROR_CLIENT -> "client_error"
        android.speech.SpeechRecognizer.ERROR_NETWORK -> "network_error"
        android.speech.SpeechRecognizer.ERROR_NETWORK_TIMEOUT -> "network_timeout"
        android.speech.SpeechRecognizer.ERROR_RECOGNIZER_BUSY -> "recognizer_busy"
        android.speech.SpeechRecognizer.ERROR_SERVER -> "server_error"
        else -> "speech_error_$error"
    }

    @Synchronized
    private fun releaseRecognizer() {
        recognizer?.let {
            try {
                it.destroy()
            } catch (e: Exception) {
                // destroy 失败没有可做的补救：引用照样丢掉。
            }
        }
        recognizer = null
    }

    // ─────────────────────────── 常亮 ───────────────────────────

    private val screenReasons = mutableSetOf<String>()
    private val cpuReasons = mutableSetOf<String>()
    private var wakeLock: android.os.PowerManager.WakeLock? = null

    /**
     * 加/减一个唤醒理由。display = true 用 FLAG_KEEP_SCREEN_ON（屏幕常亮）；
     * false 用 PARTIAL_WAKE_LOCK（只保 CPU）。理由可嵌套：同名理由加两次就要减两次，
     * 减到空才真正放开——与平台契约一致。
     *
     * 返回 0 = 已生效；1 = 没有 Activity。
     */
    @Synchronized
    fun keepAwake(display: Boolean, reason: ByteArray, on: Boolean): Int {
        val ctx = activity ?: return 1
        val key = String(reason, Charsets.UTF_8)
        if (display) {
            val changed = if (on) screenReasons.add(key) else screenReasons.remove(key)
            if (changed) onUi { applyKeepScreenOn(screenReasons.isNotEmpty()) }
            return 0
        }
        val changed = if (on) cpuReasons.add(key) else cpuReasons.remove(key)
        if (changed) applyCpuAwake(ctx, cpuReasons.isNotEmpty())
        return 0
    }

    private fun applyKeepScreenOn(on: Boolean) {
        val ctx = activity ?: return
        if (on) {
            ctx.window.addFlags(android.view.WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        } else {
            ctx.window.clearFlags(android.view.WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        }
    }

    @Synchronized
    private fun applyCpuAwake(ctx: Context, on: Boolean) {
        if (on) {
            if (wakeLock == null) {
                val pm = ctx.getSystemService(Context.POWER_SERVICE) as android.os.PowerManager
                wakeLock = pm.newWakeLock(android.os.PowerManager.PARTIAL_WAKE_LOCK, "mygo:awake").apply {
                    setReferenceCounted(false)
                    acquire()
                }
            }
        } else {
            wakeLock?.let { if (it.isHeld) it.release() }
            wakeLock = null
        }
    }

    /**
     * 是否正在用电池。插着但没充（充满、或充电器太弱）也算在用电池：
     * 这里看的是 status，而不是 plugged。
     */
    fun onBattery(): Boolean {
        val ctx = activity ?: return false
        val intent = ctx.registerReceiver(null, android.content.IntentFilter(Intent.ACTION_BATTERY_CHANGED))
            ?: return false
        val status = intent.getIntExtra(android.os.BatteryManager.EXTRA_STATUS, -1)
        val charging = status == android.os.BatteryManager.BATTERY_STATUS_CHARGING ||
            status == android.os.BatteryManager.BATTERY_STATUS_FULL
        return !charging
    }

    private var pendingPhotoToken: Long = 0
    private var pendingPhotoMultiple = false
    private var pendingExportToken: Long = 0
    private var pendingExportSource: String? = null

    /**
     * 打开系统照片选择器。token 由 Go 侧生成，结果经 takeMediaResult 返回。
     * 返回 true = 已启动；false = 启动失败（Go 侧据此报 ErrPickerUnavailable）。
     */
    fun pickPhotos(token: Long, multiple: Boolean): Boolean {
        val ctx = activity ?: return false
        pendingPhotoToken = token
        pendingPhotoMultiple = multiple
        val intent = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            Intent(MediaStore.ACTION_PICK_IMAGES).apply {
                type = "image/*"
                if (multiple) putExtra(Intent.EXTRA_ALLOW_MULTIPLE, true)
            }
        } else {
            Intent(Intent.ACTION_GET_CONTENT).apply {
                type = "image/*"
                addCategory(Intent.CATEGORY_OPENABLE)
                if (multiple) putExtra(Intent.EXTRA_ALLOW_MULTIPLE, true)
            }
        }
        return try {
            ctx.startActivityForResult(intent, PHOTOS_REQUEST)
            true
        } catch (e: Exception) {
            pendingPhotoToken = 0
            false
        }
    }

    /**
     * 打开系统文档选择器，把一个已存在的文件导出到用户选的位置。
     * 返回 true = 已启动。
     */
    fun exportFile(token: Long, path: ByteArray, title: ByteArray): Boolean {
        val ctx = activity ?: return false
        pendingExportToken = token
        pendingExportSource = String(path, Charsets.UTF_8)
        val name = String(title, Charsets.UTF_8).ifEmpty { "export" }
        val intent = Intent(Intent.ACTION_CREATE_DOCUMENT).apply {
            addCategory(Intent.CATEGORY_OPENABLE)
            type = "*/*"
            putExtra(Intent.EXTRA_TITLE, name)
        }
        return try {
            ctx.startActivityForResult(intent, EXPORT_REQUEST)
            true
        } catch (e: Exception) {
            pendingExportToken = 0
            pendingExportSource = null
            false
        }
    }

    /** Activity 的 onActivityResult 转发进来：选图与导出。 */
    fun onMediaResult(requestCode: Int, resultCode: Int, data: Intent?) {
        when (requestCode) {
            PHOTOS_REQUEST -> {
                val token = pendingPhotoToken
                val multiple = pendingPhotoMultiple
                pendingPhotoToken = 0
                pendingPhotoMultiple = false
                if (token == 0L) return
                if (resultCode != Activity.RESULT_OK || data == null) {
                    deliverMedia(0, token, "")
                    return
                }
                val uris = ArrayList<Uri>()
                val clip = data.clipData
                if (multiple && clip != null) {
                    for (i in 0 until clip.itemCount) {
                        clip.getItemAt(i)?.uri?.let { uris.add(it) }
                    }
                } else {
                    data.data?.let { uris.add(it) }
                }
                val paths = ArrayList<String>(uris.size)
                for (i in uris.indices) {
                    copyToCache(uris[i], "photo-$token-$i")?.let { paths.add(it) }
                }
                deliverMedia(0, token, paths.joinToString("\n"))
            }
            EXPORT_REQUEST -> {
                val token = pendingExportToken
                val source = pendingExportSource
                pendingExportToken = 0
                pendingExportSource = null
                if (token == 0L) return
                val target = data?.data
                if (resultCode != Activity.RESULT_OK || target == null || source == null) {
                    deliverMedia(1, token, "0")
                    return
                }
                deliverMedia(1, token, if (copyToDocument(target, source)) "1" else "0")
            }
        }
    }

    private fun copyToCache(uri: Uri, name: String): String? {
        val ctx = activity ?: return null
        return try {
            val dir = File(ctx.cacheDir, "mygo-media").apply { mkdirs() }
            val ext = ctx.contentResolver.getType(uri)?.substringAfterLast('/') ?: "bin"
            val f = File(dir, "$name.$ext")
            ctx.contentResolver.openInputStream(uri)?.use { input ->
                f.outputStream().use { output -> input.copyTo(output) }
            } ?: return null
            f.absolutePath
        } catch (e: Exception) {
            null
        }
    }

    private fun copyToDocument(target: Uri, source: String): Boolean {
        val ctx = activity ?: return false
        return try {
            ctx.contentResolver.openOutputStream(target)?.use { output ->
                File(source).inputStream().use { input -> input.copyTo(output) }
            } ?: return false
            true
        } catch (e: Exception) {
            false
        }
    }

    // ─────────────────────────── 打开 URL ───────────────────────────

    /**
     * 用 ACTION_VIEW 打开一个 URL。返回 0 = 已交给系统；1 = 没有应用能处理；
     * 2 = 被拒绝（非法 URL / 系统拒绝启动）。
     *
     * 🔴 **安全边界（与 Go 侧 validateExternalURL 是两道独立的检查）**
     * 参数来自不可信输入（可能是页面给的），所以：
     *  · **scheme 白名单**：只允许 http / https / mailto / tel。file:// content://
     *    intent:// android-app:// package: market: data: javascript: blob: 一律拒绝 ——
     *    这些 scheme 指向的不是网页而是**组件或本机资源**，直接拼进 Intent 就等于把
     *    「打开网页」变成「启动某个组件 / 读某个文件」。
     *  · **不 setPackage / 不 setComponent**：Intent 保持隐式，由系统挑处理器；显式
     *    指定包名会把 URL 变成「启动指定应用」的手段。
     *  · **不加任何 extras、不带 clipData**：不给接收方留第二个未经检查的字符串。
     *  · http/https 额外加 CATEGORY_BROWSABLE，让只有声明了该 category 的浏览器类应用
     *    能收到；mailto/tel 不加（邮件/拨号应用通常不声明 BROWSABLE，加了会解析不到）。
     *  · 用 `Uri.parse` 之前先逐字符拒绝控制字符与首尾空白，避免 "https://x\n…" 这类
     *    靠换行绕过后续校验的形状。
     *
     * 🌐 注：Android 11+ 的**包可见性**（package visibility）会影响
     * `resolveActivity()` 这类查询；本实现**不查询**、直接 startActivity 并捕获
     * ActivityNotFoundException，所以**不需要** `<queries>` 声明。请不要为了「先判断有没有
     * 浏览器」而改成 resolveActivity —— 那才会需要 Manifest 补丁，且在这里没有收益。
     */
    fun openExternalUrl(url: ByteArray): Int {
        val ctx = activity ?: return 2
        val raw = String(url, Charsets.UTF_8)
        val scheme = validatedScheme(raw) ?: return 2
        val intent = Intent(Intent.ACTION_VIEW, Uri.parse(raw))
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        if (scheme == "http" || scheme == "https") {
            intent.addCategory(Intent.CATEGORY_BROWSABLE)
        }
        return try {
            ctx.startActivity(intent)
            0
        } catch (e: ActivityNotFoundException) {
            1
        } catch (e: SecurityException) {
            2
        }
    }

    /**
     * 与 Go 侧 `validateExternalURL` 同规则的第二道检查：返回小写 scheme，非法返回 null。
     *
     * 两道检查存在的原因不同：Go 侧那道是契约的一部分（不可信输入不许变成 Intent），
     * 这一道是「真正构造 Intent 的进程不许跳过检查」。两边任一改动都要同步改另一边。
     */
    private fun validatedScheme(raw: String): String? {
        if (raw.isEmpty()) return null
        if (raw.any { it == '\u0000' || it == '\r' || it == '\n' || it == '\t' }) return null
        if (raw != raw.trim()) return null
        val colon = raw.indexOf(':')
        if (colon <= 0) return null
        val scheme = raw.substring(0, colon).lowercase()
        if (scheme !in ALLOWED_SCHEMES) return null
        val rest = raw.substring(colon + 1)
        when (scheme) {
            "http", "https" -> {
                // 必须形如 scheme://host；"https:" 或 "https:///" 没有可加载的主机。
                if (!rest.startsWith("//")) return null
                val host = rest.substring(2).substringBefore('/').substringBefore('?').substringBefore('#')
                if (host.isEmpty()) return null
            }
            "mailto", "tel" -> if (rest.isEmpty()) return null
        }
        return scheme
    }

    private val ALLOWED_SCHEMES = setOf("http", "https", "mailto", "tel")

    // ─────────────────────────── 通知 ───────────────────────────

    /**
     * 当前是否允许发通知。
     *
     * Android 13（API 33）起是 **POST_NOTIFICATIONS 运行时权限**；13 以下这个权限在平台上
     * 根本不存在（checkSelfPermission 会返回 DENIED），此时看
     * NotificationManager.areNotificationsEnabled()（用户可以在设置里关掉整个渠道）。
     * 两条都查，才是「真的能不能发出来」。
     */
    fun notificationsAllowed(): Boolean {
        val ctx = activity ?: return false
        return notificationsAllowed(ctx)
    }

    /** 同一判断，给没有 Activity 的路径用（推送把进程拉起时）。 */
    private fun notificationsAllowed(ctx: Context): Boolean {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            val granted = ctx.checkSelfPermission(Manifest_permission_POST_NOTIFICATIONS()) ==
                PackageManager.PERMISSION_GRANTED
            if (!granted) return false
        }
        val nm = notificationManager(ctx) ?: return false
        return nm.areNotificationsEnabled()
    }

    /**
     * 发一条通知。返回 0 = 已交给系统；1 = 权限没给（调用方应报
     * platform.ErrNotificationsDenied）；2 = 其它失败。
     *
     * 🔴 **Android 13+ 没权限时 notify() 会被系统接受并丢弃**，不抛异常、不返回错误 ——
     * 这正是「静默失败」的来源。所以这里在发之前显式查权限，缺权限就返回 1，让 Go 侧
     * 把 ErrNotificationsDenied 报给调用方，而不是让应用以为发成功了。
     */
    fun postNotification(json: ByteArray): Int {
        val ctx = activity ?: return 2
        return postNotification(ctx, json)
    }

    /**
     * 同一个入口，给没有 Activity 的路径用：闹钟把进程拉起时，只有 Context。
     */
    private fun postNotification(ctx: Context, json: ByteArray): Int {
        val nm = notificationManager(ctx) ?: return 2
        if (!notificationsAllowed(ctx)) return 1
        val obj = try {
            JSONObject(String(json, Charsets.UTF_8))
        } catch (e: Exception) {
            return 2
        }
        val delay = obj.optDouble("delay", 0.0)
        if (delay > 0) {
            return scheduleNotification(ctx, obj, delay)
        }
        val tag = obj.optString("id")
        val silent = obj.optBoolean("silent", false)
        val urgent = obj.optBoolean("urgent", false)
        val builder = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            // 静默/告警与否在 8.0+ 由**渠道**决定，不能按条覆盖，所以各建一条
            // （见 createNotificationChannel）：否则用户设了「有声音」，任何一条都静不了。
            Notification.Builder(ctx, channelFor(silent, urgent))
        } else {
            Notification.Builder(ctx)
        }
        builder
            .setContentTitle(obj.optString("title"))
            .setContentText(obj.optString("body"))
            .setAutoCancel(true)
            .setSmallIcon(smallIcon(ctx))
            .setContentIntent(clickIntent(ctx, tag))
        if (obj.has("progress") && !obj.isNull("progress")) {
            val done = Math.round(obj.optDouble("progress", 0.0).coerceIn(0.0, 1.0) * 100).toInt()
            builder.setProgress(100, done, false)
        }
        obj.optJSONArray("actions")?.let { actions ->
            for (i in 0 until actions.length()) {
                val action = actions.optJSONObject(i) ?: continue
                val actionID = action.optString("id")
                val label = action.optString("label")
                if (actionID.isEmpty() || label.isEmpty()) continue
                builder.addAction(
                    Notification.Action.Builder(
                        Icon.createWithResource(ctx, smallIcon(ctx)),
                        label,
                        actionIntent(ctx, tag, actionID),
                    ).build(),
                )
            }
        }
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) {
            // 8.0 以下没有渠道：静默 = 不要声音、不要震动、不要灯。
            @Suppress("DEPRECATION")
            when {
                silent -> builder.setSound(null).setDefaults(0)
                urgent -> builder.setDefaults(Notification.DEFAULT_ALL)
                else -> builder.setDefaults(Notification.DEFAULT_SOUND)
            }
        }
        return try {
            // tag = 应用给的 id，int id 恒为 0：由 tag 区分不同通知，避免把字符串 id
            // 哈希成 int 之后碰撞。
            nm.notify(tag, NOTIFICATION_ID, builder.build())
            0
        } catch (e: RuntimeException) {
            2
        }
    }

    /**
     * 定时通知：交给系统闹钟，进程被杀也照常到点。能精确就精确，系统不允许
     * 精确闹钟（Android 12+ 用户可关）时退到 setAndAllowWhileIdle。
     *
     * 存进 PendingIntent 的 JSON 会先去掉 delay —— 否则闹钟到点后又会排一次。
     */
    private fun scheduleNotification(ctx: Context, obj: JSONObject, delaySeconds: Double): Int {
        val alarm = ctx.getSystemService(Context.ALARM_SERVICE) as? AlarmManager ?: return 2
        val id = obj.optString("id")
        val at = System.currentTimeMillis() + (delaySeconds * 1000).toLong()
        val fire = JSONObject(obj.toString()).apply { remove("delay") }
        val intent = Intent(ctx, NotificationAlarmReceiver::class.java)
            .setAction(ACTION_NOTIFICATION_ALARM)
            .setData(Uri.parse("mygo://notification-alarm/" + Uri.encode(id)))
            .putExtra(EXTRA_NOTIFICATION_JSON, fire.toString().toByteArray(Charsets.UTF_8))
        val pending = PendingIntent.getBroadcast(
            ctx,
            id.hashCode(),
            intent,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
        return try {
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S && !alarm.canScheduleExactAlarms()) {
                alarm.setAndAllowWhileIdle(AlarmManager.RTC_WAKEUP, at, pending)
            } else {
                alarm.setExactAndAllowWhileIdle(AlarmManager.RTC_WAKEUP, at, pending)
            }
            0
        } catch (e: Exception) {
            2
        }
    }

    /** 定时通知到点：把存下的 JSON 当普通通知发出去。 */
    class NotificationAlarmReceiver : BroadcastReceiver() {
        override fun onReceive(context: Context, intent: Intent) {
            if (intent.action != ACTION_NOTIFICATION_ALARM) return
            val json = intent.getByteArrayExtra(EXTRA_NOTIFICATION_JSON) ?: return
            MygoPlatform.postNotification(context, json)
        }
    }

    /** 通知上的按钮：点击回传 id + action，并把这条通知收掉。 */
    private fun actionIntent(ctx: Context, id: String, action: String): PendingIntent {
        val intent = Intent(ctx, NotificationReceiver::class.java)
            .setAction(ACTION_NOTIFICATION_ACTION)
            .setData(Uri.parse("mygo://notification/" + Uri.encode(id) + "/" + Uri.encode(action)))
            .putExtra(EXTRA_NOTIFICATION_ID, id)
            .putExtra(EXTRA_NOTIFICATION_ACTION, action)
        return PendingIntent.getBroadcast(
            ctx,
            (id + "/" + action).hashCode(),
            intent,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
    }

    private fun channelFor(silent: Boolean, urgent: Boolean): String = when {
        urgent -> CHANNEL_ID_ALERT
        silent -> CHANNEL_ID_SILENT
        else -> CHANNEL_ID
    }

    /**
     * FCM 消息到达。带标题或正文的（notification 块，或 data 里的 title/body）
     * 直接落本地通知 —— 进程被杀、没有 Activity 时也能弹；纯数据型交给 Go，
     * 只有应用活着才有意义。
     *
     * 没有 Activity 正是推送拉起进程时的常态，所以这里只用 [context]，
     * 不碰 [activity]。
     */
    fun onPushMessage(context: Context, id: String, title: String, body: String, data: Map<String, String>) {
        if (title.isEmpty() && body.isEmpty()) {
            nativePushMessage(JSONObject(data).toString().toByteArray(Charsets.UTF_8))
            return
        }
        postPushNotification(context, id.ifEmpty { data["id"].orEmpty() }, title, body, data)
    }

    /**
     * 推送通知：渠道按 data["urgency"]（high/alert 走告警通道），点击时如果
     * data 里有 url，就带 ACTION_VIEW 打开 Activity —— 冷启动会走现有的
     * OnOpenURL 路径，与扫码/深链是同一条。没有 url 就普通打开。
     */
    private fun postPushNotification(
        context: Context,
        id: String,
        title: String,
        body: String,
        data: Map<String, String>,
    ) {
        if (!notificationsAllowed(context)) return
        val nm = notificationManager(context) ?: return
        createNotificationChannel(context)
        val urgent = data["urgency"] == "high" || data["urgency"] == "alert"
        val silent = data["silent"] == "true"
        val channel = channelFor(silent, urgent)
        val tag = id.ifEmpty { "push-" + System.currentTimeMillis() }
        val builder = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            Notification.Builder(context, channel)
        } else {
            Notification.Builder(context)
        }
        builder
            .setContentTitle(title)
            .setContentText(body)
            .setAutoCancel(true)
            .setSmallIcon(smallIcon(context))
            .setContentIntent(pushClickIntent(context, data["url"].orEmpty(), tag))
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) {
            @Suppress("DEPRECATION")
            if (silent) {
                builder.setSound(null).setDefaults(0)
            } else {
                builder.setDefaults(Notification.DEFAULT_ALL)
            }
        }
        try {
            nm.notify(tag, NOTIFICATION_ID, builder.build())
        } catch (e: RuntimeException) {
            // 没有通知权限时有的 ROM 会抛；推送本身已经处理完，吞掉即可。
        }
    }

    private fun pushClickIntent(context: Context, url: String, id: String): PendingIntent {
        // 启动哪个 Activity 由宿主决定：桥不认识 MyGo 自己的 MygoActivity，游戏引擎
        // 宿主也只有一个自己的 Activity。用系统给的启动 Intent 就不会绑死。
        val intent = context.packageManager.getLaunchIntentForPackage(context.packageName)
            ?: Intent(Intent.ACTION_MAIN)
                .setPackage(context.packageName)
                .addCategory(Intent.CATEGORY_LAUNCHER)
        intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP)
        if (url.isNotEmpty()) {
            intent.action = Intent.ACTION_VIEW
            intent.data = Uri.parse(url)
        }
        return PendingIntent.getActivity(
            context,
            id.hashCode(),
            intent,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
    }

    /** 撤销某条通知（tag 就是原通知的 id）。 */
    fun cancelNotification(id: ByteArray): Boolean {
        val ctx = activity ?: return false
        val nm = notificationManager(ctx) ?: return false
        nm.cancel(String(id, Charsets.UTF_8), NOTIFICATION_ID)
        return true
    }

    /** 撤销本应用的所有通知。 */
    fun cancelAllNotifications(): Boolean {
        val ctx = activity ?: return false
        val nm = notificationManager(ctx) ?: return false
        nm.cancelAll()
        return true
    }

    /**
     * 请求通知权限（Android 13+）。**只有 Activity 能弹运行时权限对话框**，所以这个方法
     * 必须由 Activity 调用（见产出里的 MygoActivity 补丁）。13 以下什么都不做：那个版本
     * 没有这个权限可请求。
     */
    fun requestNotificationPermission(requestCode: Int) {
        val ctx = activity ?: return
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU) return
        if (notificationsAllowed()) return
        ctx.requestPermissions(arrayOf(Manifest_permission_POST_NOTIFICATIONS()), requestCode)
    }

    /**
     * 通知渠道（Android 8+ 必须建，否则 notify() 静默什么都不显示 —— 又一个静默失败点）。
     *
     * 建**两条**：默认音的一条、静默的一条。8.0 之后一条通知有没有声音由**渠道**决定，
     * 按条设置无效（framework 的 Notification.Builder 连 setSilent 都没有，那是
     * AndroidX 的 NotificationCompat 才有的），所以「静默通知」只能靠换渠道实现。
     * 渠道一旦创建，重要性就只能由用户在设置里改，所以这里不替用户决定，用默认重要性。
     */
    private fun createNotificationChannel(ctx: Context) {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return
        val nm = notificationManager(ctx) ?: return
        if (nm.getNotificationChannel(CHANNEL_ID) == null) {
            nm.createNotificationChannel(
                NotificationChannel(CHANNEL_ID, "mygo", NotificationManager.IMPORTANCE_DEFAULT)
            )
        }
        if (nm.getNotificationChannel(CHANNEL_ID_SILENT) == null) {
            nm.createNotificationChannel(
                NotificationChannel(
                    CHANNEL_ID_SILENT,
                    "mygo (silent)",
                    NotificationManager.IMPORTANCE_LOW
                )
            )
        }
        // 告警通道：摄像头/门磁这类事件要打断用户，而不是排队等。
        if (nm.getNotificationChannel(CHANNEL_ID_ALERT) == null) {
            nm.createNotificationChannel(
                NotificationChannel(
                    CHANNEL_ID_ALERT,
                    "mygo (alert)",
                    NotificationManager.IMPORTANCE_HIGH
                )
            )
        }
    }

    private fun notificationManager(ctx: Context): NotificationManager? =
        ctx.getSystemService(Context.NOTIFICATION_SERVICE) as? NotificationManager

    /**
     * 通知的小图标。
     *
     * 通知**必须**有 smallIcon，否则 Android 会拒绝显示（并且不报错）。宿主工程没有自带
     * 图标资源，所以用应用自己的图标（applicationInfo.icon）；取不到时退到系统图标。
     * 真要上架时应当换成一张单色通知图标并加进 res/（Manifest/res 不归本条线）。
     */
    private fun smallIcon(ctx: Context): Int {
        val icon = ctx.applicationInfo?.icon ?: 0
        return if (icon != 0) icon else android.R.drawable.ic_dialog_info
    }

    /**
     * 点击通知的 PendingIntent：指向本文件里的 [NotificationReceiver]。
     *
     * · 用 **setData** 带上 id：PendingIntent 的「同一个」判定不看 extras，只按
     *   action/data/component 等比较；只塞 extra 的话不同通知会互相覆盖，点谁都回同一个 id。
     * · **FLAG_IMMUTABLE**：Android 12+ 强制要求显式可变性（缺了会抛
     *   IllegalArgumentException），本场景不需要接收方改内容，所以不可变。
     * · 接收方要能收到，需要在 Manifest 里声明 [NotificationReceiver]（见产出补丁）；
     *   没有声明时点击通知会没有任何反应 —— 这也是本波明确写出来的接线缺口，而不是静默。
     */
    private fun clickIntent(ctx: Context, id: String): PendingIntent {
        val intent = Intent(ctx, NotificationReceiver::class.java)
            .setAction(ACTION_NOTIFICATION_CLICK)
            .setData(Uri.parse("mygo://notification/${Uri.encode(id)}"))
            .putExtra(EXTRA_NOTIFICATION_ID, id)
        return PendingIntent.getBroadcast(
            ctx,
            0,
            intent,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
        )
    }

    /**
     * 通知被点击时的接收方。
     *
     * 用 BroadcastReceiver 而不是 Activity：Activity 没在运行时（应用被系统回收后）点击
     * 通知也会走到这里，Go 侧的 AppHandler.NotificationClicked 因此不会漏。
     * 需要在 Manifest 里注册（`android:exported="false"`，只由本应用的通知触发）。
     */
    class NotificationReceiver : BroadcastReceiver() {
        override fun onReceive(context: Context, intent: Intent) {
            val id = intent.getStringExtra(EXTRA_NOTIFICATION_ID) ?: return
            val nm = context.getSystemService(Context.NOTIFICATION_SERVICE) as? NotificationManager
            nm?.cancel(id, NOTIFICATION_ID)
            when (intent.action) {
                // Go 侧：AppHandler.NotificationClicked(id)。
                ACTION_NOTIFICATION_CLICK -> MygoPlatform.notificationClicked(id)
                // Go 侧：OnNotification 事件，Action 是按钮的 id。
                ACTION_NOTIFICATION_ACTION -> {
                    val action = intent.getStringExtra(EXTRA_NOTIFICATION_ACTION) ?: return
                    MygoPlatform.notificationAction(id, action)
                }
            }
        }
    }

    /** 通知被点击：把应用给的 id（UTF-8 字节）交给 Go 侧。 */
    fun notificationClicked(id: String) {
        nativeNotificationClicked(id.toByteArray(Charsets.UTF_8))
    }

    /** 通知上的按钮被按下：id 与按钮的 action 一起交给 Go 侧。 */
    fun notificationAction(id: String, action: String) {
        nativeNotificationAction(id.toByteArray(Charsets.UTF_8), action.toByteArray(Charsets.UTF_8))
    }

    /** 无障碍服务打开：让 Go 侧开始为每一帧生成节点树。 */
    private external fun nativeAccessibilityOn()

    /** 无障碍动作：把动作名（UTF-8 字节）交给 Go 侧。 */
    fun accessibilityAction(id: Long, action: String) {
        nativeAccessibilityAction(id, action.toByteArray(Charsets.UTF_8))
    }

    // ─────────────────────────── 无障碍 ───────────────────────────

    /**
     * AccessibilityManager：判断无障碍服务是否开启。TalkBack 没开时整套 provider 都不会
     * 被调用，Go 侧也就没必要把树送过来（送也不会有人读）。
     */
    private fun accessibilityEnabled(ctx: Context): Boolean {
        val am = ctx.getSystemService(Context.ACCESSIBILITY_SERVICE) as? AccessibilityManager
        return am?.isEnabled == true
    }

    /**
     * 把 Go 侧序列化好的 AccessTree（见 internal/android/backend/platform.go 的
     * encodeAccessTree）交给 Java 侧保存，供 provider 服务。
     *
     * 🔴 **为什么必须走 provider**（Android 官方硬要求）：像本工程这样用自绘 SurfaceView
     * 呈现内容的应用，View 里没有任何子 View，TalkBack 读到的就是「一个空白 View」——
     * 完全不可用。必须 override getAccessibilityNodeProvider() 或使用
     * ExploreByTouchHelper 把自绘内容暴露成虚拟节点，否则无障碍是彻底失效的。
     *
     * 本实现用**框架自带的** AccessibilityNodeProvider（不依赖 AndroidX，因此
     * **不需要**动 build.gradle 加 ExploreByTouchHelper 依赖）。
     */
    fun setAccessibilityTree(json: ByteArray) {
        val tree = AccessTree.parse(String(json, Charsets.UTF_8)) ?: return
        val ctx = activity ?: return
        if (!accessibilityEnabled(ctx)) return
        if (!accessibilityTreeLogged && tree.nodes.isNotEmpty()) {
            accessibilityTreeLogged = true
            Log.i("mygo", "accessibility tree: " + tree.nodes.size + " nodes")
        }
        nodes = tree
        onUi {
            val view = host ?: return@onUi
            // 内容变了：让 TalkBack 重新取节点。类型用 WINDOW_CONTENT_CHANGED，
            // 这是自绘内容更新时的正确事件。
            view.sendAccessibilityEvent(AccessibilityEvent.TYPE_WINDOW_CONTENT_CHANGED)
        }
    }

    /** TalkBack 要求朗读一段文字（announcement），读在它当前朗读内容之后。 */
    fun announce(text: ByteArray) {
        val message = String(text, Charsets.UTF_8)
        if (message.isEmpty()) return
        onUi { host?.announceForAccessibility(message) }
    }

    /** 当前无障碍树；@Volatile 因为写来自 Go 的线程、读来自 UI/无障碍线程。 */
    @Volatile
    private var nodes: AccessTree? = null

    /** 已经告诉过 Go「无障碍开着」；服务关掉后重置，再开时重发一次。 */
    @Volatile
    private var accessibilityAnnounced = false

    /** 状态监听只注册一次：install 会在每个 Activity 上都调用。 */
    @Volatile
    private var accessibilityListening = false

    /** 只记第一次的节点数，免得每帧刷日志。 */
    @Volatile
    private var accessibilityTreeLogged = false

    private fun notifyAccessibilityOn() {
        val ctx = activity ?: return
        if (accessibilityAnnounced || !accessibilityEnabled(ctx)) return
        accessibilityAnnounced = true
        nativeAccessibilityOn()
    }

    /** 无障碍焦点（虚拟节点 id），TalkBack 移动焦点时更新。 */
    @Volatile
    private var accessibilityFocusId: Int = View.NO_ID

    private fun clearAccessibilityTree() {
        nodes = null
        accessibilityFocusId = View.NO_ID
        MygoPlatform.virtualIds.reset()
    }

    /**
     * 挂在宿主 View 上的 AccessibilityDelegate：提供虚拟节点。
     *
     * 这是「自绘 View 对 TalkBack 可用」的入口点。用 View.AccessibilityDelegate 而不是让
     * MygoView 直接 override：MygoView 不归本条线（也不该知道无障碍的细节），
     * `view.accessibilityDelegate = …` 一行就能装上。
     */
    private class MygoAccessibilityDelegate : View.AccessibilityDelegate() {
        override fun getAccessibilityNodeProvider(host: View): AccessibilityNodeProvider =
            MygoNodeProvider()
    }

    /**
     * 虚拟节点 id 的分配：Go 侧的节点 id 是 uint64 且跨帧稳定，而 provider 的虚拟 id 是
     * int 且只需在本进程内唯一。这里做一次映射并**永不复用**（TalkBack 会跨帧持有 id，
     * 复用会让它认错节点）；映射表在进程内缓慢增长，一屏内容的节点数是有界的。
     */
    private object virtualIds {
        private val toVirtual = HashMap<Long, Int>()
        private val toNative = HashMap<Int, Long>()
        private var next = 1

        @Synchronized
        fun of(id: Long): Int {
            toVirtual[id]?.let { return it }
            val v = next++
            toVirtual[id] = v
            toNative[v] = id
            return v
        }

        @Synchronized
        fun native(virtual: Int): Long? = toNative[virtual]

        @Synchronized
        fun reset() {
            toVirtual.clear()
            toNative.clear()
            next = 1
        }
    }

    /**
     * AccessibilityNodeProvider：把 JSON 树变成 AccessibilityNodeInfo。
     *
     * 覆盖范围（**如实**，不夸大）：
     *  · 有：source/parent/children、className（按 role 映射）、contentDescription
     *    （label，另把 description 接在后面）、text（value）、hintText（placeholder）、
     *    boundsInParent/InScreen（DIP→px 且加上宿主 View 在屏幕上的位置）、
     *    enabled/disabled、focusable、checkable/checked、selected、scrollable、
     *    editable/password/multiLine、heading、collectionItemInfo（posInSet/setSize）、
     *    rangeInfo（slider/progress/meter/stepper）、clickable 与点击/焦点/前后滚动动作。
     *  · 无（本波未做，写在这里以免被当成「已支持」）：文字选区（selStart/selEnd）、
     *    ACTION_SET_PROGRESS 的值回传（wire 上没有携带值的通道）、
     *    表格的行列跨度（RoleTable 的 rowSpan 之类，平台契约里也没有）、
     *    以及**任何点击/动作的实际效果** —— 动作已经送到 Go 侧，但 Go 侧没有消费者
     *    （见 internal/android/backend/platform.go 的 SetAccessActionSink 说明）。
     */
    private class MygoNodeProvider : AccessibilityNodeProvider() {

        override fun createAccessibilityNodeInfo(virtualViewId: Int): AccessibilityNodeInfo? {
            val host = MygoPlatform.host ?: return null
            val tree = MygoPlatform.nodes ?: return null

            if (virtualViewId == HOST_VIEW_ID) {
                // 宿主节点本身：包住所有顶层节点。没有它，TalkBack 找不到入口。
                val info = AccessibilityNodeInfo.obtain(host)
                host.onInitializeAccessibilityNodeInfo(info)
                for (node in tree.roots()) {
                    info.addChild(host, MygoPlatform.virtualIds.of(node.id))
                }
                return info
            }

            val nativeId = MygoPlatform.virtualIds.native(virtualViewId) ?: return null
            val node = tree.node(nativeId) ?: return null

            val info = AccessibilityNodeInfo.obtain()
            info.packageName = host.context.packageName
            info.setSource(host, virtualViewId)
            // 顶层节点挂在宿主上；其余挂在各自的父节点上。Go 侧的 Parent 是**下标**
            // （-1 = 顶层），不是 id，所以这里要下标→id 转一次。
            val parentId = tree.parentIdOf(node)
            if (parentId == null) {
                info.setParent(host)
            } else {
                info.setParent(host, MygoPlatform.virtualIds.of(parentId))
            }
            for (childId in tree.childIdsOf(node)) {
                info.addChild(host, MygoPlatform.virtualIds.of(childId))
            }

            info.className = roleClassName(node.role)
            val description = if (node.description.isEmpty()) node.label else "${node.label}, ${node.description}"
            if (description.isNotEmpty()) info.contentDescription = description
            if (node.value.isNotEmpty()) info.text = node.value
            if (node.placeholder.isNotEmpty()) info.hintText = node.placeholder

            val scale = host.resources.displayMetrics.density
            val location = IntArray(2)
            host.getLocationOnScreen(location)
            val bounds = Rect(
                location[0] + (node.bounds.left * scale).toInt(),
                location[1] + (node.bounds.top * scale).toInt(),
                location[0] + (node.bounds.right * scale).toInt(),
                location[1] + (node.bounds.bottom * scale).toInt()
            )
            info.setBoundsInParent(
                Rect(
                    (node.bounds.left * scale).toInt(),
                    (node.bounds.top * scale).toInt(),
                    (node.bounds.right * scale).toInt(),
                    (node.bounds.bottom * scale).toInt()
                )
            )
            info.setBoundsInScreen(bounds)

            info.isEnabled = !node.states.contains("disabled")
            info.isFocusable = node.states.contains("focusable") || node.actions.contains("focus")
            info.isFocused = tree.focus == node.id
            info.isCheckable = node.role == "checkbox" || node.role == "radio" || node.role == "switch" ||
                node.role == "togglebutton" || node.role == "menuitemcheckbox" || node.role == "menuitemradio"
            info.isChecked = node.states.contains("checked")
            info.isSelected = node.states.contains("selected")
            info.isScrollable = node.role == "scroll" || node.role == "list" || node.role == "table"
            info.isEditable = node.role == "textfield" || node.role == "combobox"
            info.isPassword = node.states.contains("password")
            info.isMultiLine = node.states.contains("multiline")
            info.isHeading = node.role == "heading"
            info.isVisibleToUser = !node.states.contains("offscreen")
            info.isAccessibilityFocused = MygoPlatform.accessibilityFocusId == virtualViewId
            info.isClickable = node.actions.contains("press")
            info.isLongClickable = false

            if (node.posInSet > 0 || node.setSize > 0) {
                info.setCollectionItemInfo(
                    AccessibilityNodeInfo.CollectionItemInfo.obtain(
                        /* rowIndex = */ node.posInSet - 1,
                        /* rowSpan = */ 1,
                        /* columnIndex = */ 0,
                        /* columnSpan = */ 1,
                        info.isHeading
                    )
                )
            }
            if (isRanged(node.role)) {
                info.rangeInfo = AccessibilityNodeInfo.RangeInfo.obtain(
                    AccessibilityNodeInfo.RangeInfo.RANGE_TYPE_FLOAT,
                    node.min.toFloat(),
                    node.max.toFloat(),
                    node.now.toFloat()
                )
            }
            if (node.actions.contains("press")) info.addAction(AccessibilityNodeInfo.ACTION_CLICK)
            if (node.actions.contains("focus")) info.addAction(AccessibilityNodeInfo.ACTION_FOCUS)
            if (node.actions.contains("increment")) info.addAction(AccessibilityNodeInfo.ACTION_SCROLL_FORWARD)
            if (node.actions.contains("decrement")) info.addAction(AccessibilityNodeInfo.ACTION_SCROLL_BACKWARD)
            return info
        }

        /**
         * TalkBack 对某个虚拟节点执行动作：转成 Go 侧的 action 名字回传。
         *
         * 名字必须与 internal/android/backend/platform.go 的 accessActionKinds 一致
         * （press / focus / increment / decrement / setvalue / scrollintoview / expand /
         * collapse），对不上 Go 侧会拒绝并记 logcat。
         */
        override fun performAction(
            virtualViewId: Int,
            action: Int,
            arguments: Bundle?
        ): Boolean {
            val host = MygoPlatform.host ?: return false
            if (virtualViewId != HOST_VIEW_ID && MygoPlatform.virtualIds.native(virtualViewId) == null) return false
            val name = when (action) {
                AccessibilityNodeInfo.ACTION_CLICK -> "press"
                AccessibilityNodeInfo.ACTION_FOCUS,
                AccessibilityNodeInfo.ACTION_ACCESSIBILITY_FOCUS -> "focus"
                AccessibilityNodeInfo.ACTION_SCROLL_FORWARD -> "increment"
                AccessibilityNodeInfo.ACTION_SCROLL_BACKWARD -> "decrement"
                else -> return false
            }
            if (action == AccessibilityNodeInfo.ACTION_ACCESSIBILITY_FOCUS) {
                // 无障碍焦点是 Java 侧的状态：TalkBack 移动焦点时要给它 FOCUSED 事件，
                // 否则它不知道自己「到了哪里」。清除旧焦点是协议要求的一半。
                sendFocusEvent(host, virtualViewId, accessibilityFocused = true)
            }
            val nativeId = MygoPlatform.virtualIds.native(virtualViewId)
            // 宿主节点上的动作（例如 CLICK）没有对应元素，不往 Go 侧送。
            if (nativeId != null) {
                MygoPlatform.accessibilityAction(nativeId, name)
            }
            return true
        }

        override fun findAccessibilityNodeInfosByText(
            text: String,
            virtualViewId: Int
        ): MutableList<AccessibilityNodeInfo> {
            val out = ArrayList<AccessibilityNodeInfo>()
            val tree = MygoPlatform.nodes ?: return out
            val needle = text.lowercase()
            for (node in tree.nodes) {
                if (node.label.lowercase().contains(needle) || node.value.lowercase().contains(needle)) {
                    createAccessibilityNodeInfo(MygoPlatform.virtualIds.of(node.id))?.let { out.add(it) }
                }
            }
            return out
        }

        /** 发焦点事件：进入/离开某个虚拟节点。 */
        private fun sendFocusEvent(host: View, virtualViewId: Int, accessibilityFocused: Boolean) {
            val type = if (accessibilityFocused) {
                AccessibilityEvent.TYPE_VIEW_ACCESSIBILITY_FOCUSED
            } else {
                AccessibilityEvent.TYPE_VIEW_ACCESSIBILITY_FOCUS_CLEARED
            }
            MygoPlatform.accessibilityFocusId = if (accessibilityFocused) virtualViewId else View.NO_ID
            val event = AccessibilityEvent.obtain(type)
            event.packageName = host.context.packageName
            event.className = MygoPlatform::class.java.name
            event.setSource(host, virtualViewId)
            host.parent?.requestSendAccessibilityEvent(host, event)
        }
    }

    /** ranged role：值在区间里，能变成 RangeInfo。 */
    private fun isRanged(role: String): Boolean =
        role == "slider" || role == "progress" || role == "meter" || role == "stepper"

    /**
     * role → Android 类名。TalkBack 靠类名决定「这是个按钮还是个文本」，也靠它选朗读措辞。
     * Go 侧没给的 role（"" 或未知）落到 View，即「普通元素」，而不是随便挑一个看起来像的。
     */
    private fun roleClassName(role: String): String = when (role) {
        "button", "togglebutton", "disclosure", "colorwell", "popupbutton", "menubutton" -> "android.widget.Button"
        "checkbox", "menuitemcheckbox" -> "android.widget.CheckBox"
        "radio", "menuitemradio" -> "android.widget.RadioButton"
        "switch" -> "android.widget.Switch"
        "text" -> "android.widget.TextView"
        "heading" -> "android.view.View"
        "link" -> "android.widget.TextView"
        "image" -> "android.widget.ImageView"
        "textfield", "search", "combobox" -> "android.widget.EditText"
        "progress", "meter" -> "android.widget.ProgressBar"
        "slider", "stepper" -> "android.widget.SeekBar"
        "list", "listitem", "grid" -> "android.widget.ListView"
        "scroll" -> "android.widget.ScrollView"
        "tab", "tablist" -> "android.widget.TabWidget"
        "menu", "menubar", "menuitem", "popup" -> "android.widget.PopupMenu"
        "toolbar" -> "android.widget.Toolbar"
        "table" -> "android.widget.TableLayout"
        "row", "cell", "columnheader" -> "android.widget.TextView"
        "tree", "treeitem" -> "android.widget.ListView"
        "dialog", "alertdialog", "tooltip" -> "android.app.Dialog"
        "status" -> "android.widget.TextView"
        "splitter" -> "android.widget.SeekBar"
        "radiogroup", "group" -> "android.view.ViewGroup"
        else -> "android.view.View"
    }

    /**
     * 无障碍树的 Java 侧副本：从 Go 侧的 JSON 解析出来。
     *
     * 形状见 internal/android/backend/platform.go 的 accessTreeJSON / accessNodeJSON
     * （`{"focus":N,"nodes":[…],"announcements":[…]}`，节点带 id/parent(下标)/role/label/
     * value/bounds[4]/states[]/actions[]/数值字段）。解析是**宽容的**：缺字段取默认值，
     * 未知 role/states/actions 原样保留为字符串（约定里 Java 侧只认自己知道的名字）。
     */
    private class AccessTree(
        val focus: Long,
        val nodes: List<Node>
    ) {
        /** 下标 → 节点，用于 parent（Go 侧是下标）与 children（自己算）。 */
        fun node(id: Long): Node? = nodes.firstOrNull { it.id == id }

        fun roots(): List<Node> = nodes.filter { it.parent < 0 || it.parent >= nodes.size }

        fun parentIdOf(node: Node): Long? =
            if (node.parent in nodes.indices) nodes[node.parent].id else null

        fun childIdsOf(node: Node): List<Long> {
            val index = nodes.indexOfFirst { it.id == node.id }
            if (index < 0) return emptyList()
            return nodes.filter { it.parent == index }.map { it.id }
        }

        companion object {
            fun parse(json: String): AccessTree? = try {
                val root = JSONObject(json)
                val array = root.optJSONArray("nodes") ?: JSONArray()
                val nodes = ArrayList<Node>(array.length())
                for (i in 0 until array.length()) {
                    val o = array.optJSONObject(i) ?: continue
                    nodes.add(Node.parse(o))
                }
                AccessTree(root.optLong("focus", 0L), nodes)
            } catch (e: Exception) {
                // 畸形 JSON 直接放弃这次更新（保持上一帧的树）：宁可少更新，也不要
                // 在无障碍线程里抛异常。
                null
            }
        }
    }

    /** 一个节点；字段与 Go 侧 accessNodeJSON 一一对应（bounds 单位是 DIP，相对 surface）。 */
    private class Node(
        val id: Long,
        val parent: Int,
        val role: String,
        val label: String,
        val value: String,
        val description: String,
        val placeholder: String,
        val bounds: Rect,
        val states: Set<String>,
        val actions: Set<String>,
        val min: Double,
        val max: Double,
        val now: Double,
        val step: Double,
        val level: Int,
        val posInSet: Int,
        val setSize: Int,
        val selStart: Int,
        val selEnd: Int
    ) {
        companion object {
            fun parse(o: JSONObject): Node {
                val b = o.optJSONArray("bounds")
                val bounds = Rect(
                    b?.optDouble(0, 0.0)?.toInt() ?: 0,
                    b?.optDouble(1, 0.0)?.toInt() ?: 0,
                    (b?.optDouble(0, 0.0) ?: 0.0).toInt() + (b?.optDouble(2, 0.0) ?: 0.0).toInt(),
                    (b?.optDouble(1, 0.0) ?: 0.0).toInt() + (b?.optDouble(3, 0.0) ?: 0.0).toInt()
                )
                return Node(
                    id = o.optLong("id", 0L),
                    parent = o.optInt("parent", -1),
                    role = o.optString("role", ""),
                    label = o.optString("label", ""),
                    value = o.optString("value", ""),
                    description = o.optString("description", ""),
                    placeholder = o.optString("placeholder", ""),
                    bounds = bounds,
                    states = o.optJSONArray("states").toStringSet(),
                    actions = o.optJSONArray("actions").toStringSet(),
                    min = o.optDouble("min", 0.0),
                    max = o.optDouble("max", 0.0),
                    now = o.optDouble("now", 0.0),
                    step = o.optDouble("step", 0.0),
                    level = o.optInt("level", 0),
                    posInSet = o.optInt("posInSet", 0),
                    setSize = o.optInt("setSize", 0),
                    selStart = o.optInt("selStart", 0),
                    selEnd = o.optInt("selEnd", 0)
                )
            }
        }
    }

    private fun JSONArray?.toStringSet(): Set<String> {
        if (this == null) return emptySet()
        val out = HashSet<String>(length())
        for (i in 0 until length()) {
            val s = optString(i, "")
            if (s.isNotEmpty()) out.add(s)
        }
        return out
    }

    // ─────────────────────────── 线程与 JNI 契约 ───────────────────────────

    /** 在主线程执行 block：已在主线程就直接跑，否则 post。 */
    private fun onUi(block: () -> Unit) {
        if (Looper.myLooper() == Looper.getMainLooper()) {
            block()
        } else {
            mainHandler.post(block)
        }
    }

    /**
     * Android 13+ 的通知权限常量。
     *
     * 单独包一层是为了在 API < 33 的**运行**设备上也安全：直接引用
     * `Manifest.permission.POST_NOTIFICATIONS` 在编译期没问题（compileSdk=36 有这个字段），
     * 但常量值在旧系统上无意义，包一层会让「只在 33+ 读它」这件事显式。
     */
    private fun Manifest_permission_POST_NOTIFICATIONS(): String =
        "android.permission.POST_NOTIFICATIONS"

    private const val SCAN_REQUEST = 0x5CA9
    private const val PERMISSION_REQUEST = 0x5CAC
    private const val SCAN_OK = 0
    private const val SCAN_CANCELED = 5
    private const val PHOTOS_REQUEST = 0x5CAA
    private const val EXPORT_REQUEST = 0x5CAB
    private const val SCAN_NOT_FOUND = 2
    private const val KEY_ALIAS = "mygo-secrets-v1"
    private const val GCM_IV_LEN = 12
    private const val GCM_TAG_BITS = 128

    // ── Java → Go：3 个 external（JNI 符号 Java_org_mygo_MygoPlatform_*）──
    // 名字/参数/返回值与 android/jni/jni_platform.go 的 //export **逐字**一致，
    // 改一个就两边一起改（javap 是核对手段，见产出）。

    /** 记下 JavaVM 与 MygoPlatform 对象，让 Go 侧可以回调本文件的普通方法。 */
    private external fun nativeAttach()

    /** 系统用 VIEW 意图打开本应用时的 URL（应用自己注册的 scheme）。 */
    private external fun nativeOpenURL(url: ByteArray)

    /** Activity 把 onCreate / onNewIntent 收到的 URL 转进来。 */
    fun deliverOpenURL(raw: String) {
        if (raw.isEmpty()) return
        nativeOpenURL(raw.toByteArray(Charsets.UTF_8))
    }

    /** 通知被点击（参数是应用给的通知 id 的 UTF-8 字节）。 */
    private external fun nativeNotificationClicked(id: ByteArray)

    /** 通知按钮：id 与 action（都用 UTF-8 字节）。 */
    private external fun nativeNotificationAction(id: ByteArray, action: ByteArray)

    /** 纯数据推送：JSON（UTF-8）交给 Go 侧的 OnPushMessage。 */
    private external fun nativePushMessage(json: ByteArray)

    /** 小组件里点了一行：行的 id（UTF-8 字节）。 */
    private external fun nativeWidgetAction(id: ByteArray)

    /** 无障碍动作（参数是动作名字的 UTF-8 字节，名字见 accessActionKinds）。 */
    private external fun nativeAccessibilityAction(id: Long, action: ByteArray)

    /** 扫码结果回传：token 对应 Go 侧的请求，code 见 ScanResult 的注释。 */

    // ── 扫码（系统相机/相册 + 解码）─────────────────────────────────────
    //
    // 不做自己的相机预览：用系统相机拍一张、或用相册里已有的照片，再解码。
    // 好处是**不需要 CAMERA 权限**（拍照的是相机应用，权限归它）、没有预览
    // 层的生命周期要管、也不用把相机栈编进来。
    //
    // 解码用 ZXing core（纯 Java，无 NDK）。它不依赖任何 UI，也不申请权限。

    /**
     * 把扫码结果存起来，等 Go 侧来取。
     *
     * 🔴 这套 bridge 是**单向**的：Go 用 jmethodID 调 Java，Java 从不回头调 Go。
     * 所以结果不能「推」过去，只能存下、由 Go 侧的 collectScanResults 轮询
     * takeScanResult 拿走。一开始我按 //export 写了推送，那个符号根本没注册，
     * 传过去的字符串也就成了垃圾。
     *
     * 编码格式："<code>\n<token>\n<text>"，与 Go 侧的 takeScanResult 对应。
     */
    private var pendingResult: String? = null

    private fun deliverScanResult(token: Long, text: String?, code: Int) {
        pendingResult = "$code\n$token\n" + (text ?: "")
        android.util.Log.i("mygo", "scan: deliver code=$code len=${text?.length ?: 0}")
    }

    /** Go 侧轮询调用：取走待发的结果，没有就返回 null。 */
    fun takeScanResult(): String? = pendingResult.also { pendingResult = null }

    private var scanToken: Long = 0
    private var pendingScanToken: Long = 0
    /** 相机要写的那个文件；onScanResult 读完就删。 */
    private var scanFile: java.io.File? = null

    /**
     * 打开系统相机拍一张。返回 false 只在连 Intent 都起不来时（没有相机应用）。
     *
     * 🔴 **不要用 `resolveActivity` 判断有没有相机。** Android 11 起包可见性
     * 会把没声明在 `<queries>` 里的包过滤掉，`resolveActivity` 就返回 null——
     * 设备上有相机也一样。这里按官方写法直接 try/catch 启动，`ActivityNotFound`
     * 才是「真没有」。Manifest 里同时声明了 queries，两者一起才可靠。
     *
     * 结果异步回到 [onScanResult]，那时才调 deliverScanResult。
     */
    fun startScan(token: Long, prompt: ByteArray): Boolean {
        val ctx = activity ?: return false
        pendingScanToken = token
        scanToken = token
        // 先开相册，不开相机。
        //
        // 理由：相册里的图是**已经拍好的、清晰的、静止的**，解出来的成功率远高于
        // 现场手持拍屏幕——实测手持拍一张，对焦稍偏 OpenCV 就解不出。桌面上
        // 显示二维码时，用系统相机拍一张再回来选，是更稳的路子；用户也能直接
        // 截屏后选那张截图。
        //
        // 相册也不行时（没有照片选择器），才退回相机。
        return try {
            ctx.startActivityForResult(
                Intent(MediaStore.ACTION_PICK_IMAGES).apply { type = "image/*" },
                SCAN_REQUEST,
            )
            android.util.Log.i("mygo", "scan: photo picker up")
            true
        } catch (e: Exception) {
            android.util.Log.w("mygo", "scan: no photo picker, trying the camera: $e")
            tryCamera(ctx)
        }
    }

    /** 拍照。相册不可用时才走这里。 */
    private fun tryCamera(ctx: Activity): Boolean = try {
        // 🔴 EXTRA_OUTPUT 必须有。没有它，ACTION_IMAGE_CAPTURE 回来的只是
        // extras["data"] 里的一张**缩略图**（约 100~200px），一张缩略图里的
        // 二维码基本解不出来。
        //
        // 🔴 路径必须是 externalCacheDir，不能用 cacheDir。私有缓存
        // （/data/data/<pkg>/cache）相机应用没有写权限，一部分厂商相机会
        // 默默写失败、然后返回 RESULT_CANCELED。
        val dir = ctx.externalCacheDir ?: ctx.cacheDir
        dir.mkdirs()
        val file = java.io.File(dir, "scan-${pendingScanToken}.jpg")
        if (file.exists()) file.delete()
        val uri = androidx.core.content.FileProvider.getUriForFile(
            ctx, "${ctx.packageName}.fileprovider", file,
        )
        scanFile = file
        ctx.startActivityForResult(
            Intent(MediaStore.ACTION_IMAGE_CAPTURE)
                .putExtra(MediaStore.EXTRA_OUTPUT, uri)
                .addFlags(Intent.FLAG_GRANT_WRITE_URI_PERMISSION),
            SCAN_REQUEST,
        )
        android.util.Log.i("mygo", "scan: camera up, output=$uri")
        true
    } catch (e: Exception) {
        android.util.Log.w("mygo", "scan: could not start the camera: $e")
        pendingScanToken = 0
        false
    }

    /** 取景界面被收起，但没有结果（用户返回）。 */
    fun cancelScan(token: Long): Boolean {
        if (pendingScanToken != token) return false
        pendingScanToken = 0
        onUi { deliverScanResult(token, null, SCAN_CANCELED) }
        return true
    }

    /**
     * Activity 的 onActivityResult 转发进来。
     *
     * 解码在后台线程做：一张 4000×3000 的照片解码要几十到几百毫秒，放在主线程
     * 会卡住界面。
     */
    fun onScanResult(requestCode: Int, resultCode: Int, data: Intent?) {
        if (requestCode != SCAN_REQUEST) {
            android.util.Log.i("mygo", "scan: ignoring result for code $requestCode")
            return
        }
        val token = pendingScanToken
        pendingScanToken = 0
        if (token == 0L) {
            android.util.Log.w("mygo", "scan: result with no pending token")
            return
        }
        val ctx = activity
        android.util.Log.i(
            "mygo", "scan: result token=$token code=$resultCode data=${data != null} file=${scanFile?.length() ?: -1}",
        )
        if (ctx == null) {
            deliverScanResult(token, null, SCAN_CANCELED)
            return
        }
        // 🔴 不看 resultCode 就判取消，是错的。厂商相机（ColorOS 实测）会用
        // EXTRA_OUTPUT 把照片写进我们给的 Uri，然后**仍然返回 RESULT_CANCELED**
        // ——文件好好的，结果码是取消。它以「文件里有没有东西」为准：
        // 有照片就去解，解不出才是「没找到码」；只有文件也没写时才算取消。
        val wrote = scanFile?.let { it.exists() && it.length() > 0 } ?: false
        // 相册回来时照片在 data.data 的 Uri 里，没有文件、可能也没有 OK：
        // 只要有「能解的东西」就不当取消。
        val haveImage = wrote || data?.data != null || data?.extras?.get("data") != null
        if (resultCode != Activity.RESULT_OK && !haveImage) {
            android.util.Log.i("mygo", "scan: canceled, no file written")
            deliverScanResult(token, null, SCAN_CANCELED)
            return
        }
        if (resultCode != Activity.RESULT_OK) {
            android.util.Log.i("mygo", "scan: resultCode=$resultCode but there is an image; decoding it")
        }
        // 三处来源，按「能解出码」的可能性排序：
        //   1. EXTRA_OUTPUT 写的那个文件 —— 全尺寸，首选
        //   2. data.extras["data"] —— 缩略图，只在没给 EXTRA_OUTPUT 时出现
        //   3. data.data 的 Uri —— 相册选的图
        @Suppress("DEPRECATION")
        val thumb = data?.extras?.get("data") as? android.graphics.Bitmap
        val uri = data?.data
        val file = scanFile
        Thread {
            val text = try {
                val bitmap = when {
                    file != null && file.length() > 0 -> android.graphics.BitmapFactory.decodeFile(file.path)
                    thumb != null -> thumb
                    uri != null -> MediaStore.Images.Media.getBitmap(ctx.contentResolver, uri)
                    else -> null
                }
                android.util.Log.i(
                    "mygo", "scan: decoded bitmap=" + (bitmap?.let { "${it.width}x${it.height}" } ?: "null"),
                )
                bitmap?.let { decodeQr(it) }
            } catch (e: Exception) {
                android.util.Log.w("mygo", "scan: decode threw: $e")
                null
            } catch (e: OutOfMemoryError) {
                android.util.Log.w("mygo", "scan: bitmap too large: $e")
                null
            }
            file?.delete()
            android.util.Log.i("mygo", "scan: text=" + (text?.take(60) ?: "null"))
            onUi {
                if (text.isNullOrEmpty()) {
                    deliverScanResult(token, null, SCAN_NOT_FOUND)
                } else {
                    deliverScanResult(token, text, SCAN_OK)
                }
            }
        }.start()
    }

    /** 用 ZXing 在一张位图里找 QR 码；找不到返回 null。 */
    private fun decodeQr(bitmap: android.graphics.Bitmap): String? {
        // ZXing core，不引 OpenCV。
        //
        // 之前一度换成 OpenCV，理由是「同一张照片 ZXing 解不出、OpenCV 能」。
        // 复测后那个结论站不住：把四个解码器放到同一批图上对照，
        // · 相册预览页那张（p5）：OpenCV 0 命中，ZXing 命中
        // · 其余清晰图：两者都命中
        // 再对规范码做 9 种劣化（模糊、运动模糊、低对比、暗光、旋转 8°、
        // 透视倾斜、JPEG q25、半尺寸、原图），两者全部通过。
        // 而 OpenCV 的 .so 是 21.6MB（APK 的三成），ZXing 的 jar 是 0.58MB。
        // 没有证据支持多付这 21MB，所以换回来。
        //
        // 下面两步仍然必要：
        //   1. 转灰度 —— RGBLuminanceSource 取的是 R 通道，不是真亮度。
        //      拍彩色屏幕时 R 通道几乎没对比度，二值化会毁掉。
        //   2. 缩到 2000px 以内 —— ZXing 逐像素扫，4096 的边没有额外收益。
        val gray = toGray(bitmap)
        val scaled = scaleDown(gray, 2000)
        val w = scaled.width
        val h = scaled.height
        val pixels = IntArray(w * h)
        scaled.getPixels(pixels, 0, w, 0, 0, w, h)
        for (i in pixels.indices) {
            val g = pixels[i] and 0xFF
            pixels[i] = (0xFF shl 24) or (g shl 16) or (g shl 8) or g
        }
        return try {
            val hints = java.util.EnumMap<com.google.zxing.DecodeHintType, Any>(com.google.zxing.DecodeHintType::class.java)
            // 试一遍再试一遍：第一次失败时 TRY_HARDER 会把旋转与更细的二值化也走一遍。
            hints[com.google.zxing.DecodeHintType.TRY_HARDER] = true
            val reader = com.google.zxing.MultiFormatReader().apply { setHints(hints) }
            reader.decode(com.google.zxing.BinaryBitmap(
                com.google.zxing.common.HybridBinarizer(
                    com.google.zxing.RGBLuminanceSource(w, h, pixels),
                ),
            )).text
        } catch (e: com.google.zxing.NotFoundException) {
            android.util.Log.i("mygo", "scan: ZXing found no code in ${w}x$h")
            null
        } catch (e: Exception) {
            android.util.Log.w("mygo", "scan: ZXing threw: $e")
            null
        }
    }

    /** 按 (0.299, 0.587, 0.114) 转灰度，与 OpenCV/PhotoShop 同公式。 */
    private fun toGray(src: android.graphics.Bitmap): android.graphics.Bitmap {
        val out = android.graphics.Bitmap.createBitmap(src.width, src.height, android.graphics.Bitmap.Config.ARGB_8888)
        val canvas = android.graphics.Canvas(out)
        val paint = android.graphics.Paint().apply {
            colorFilter = android.graphics.ColorMatrixColorFilter(
                android.graphics.ColorMatrix(
                    floatArrayOf(
                        0.299f, 0.587f, 0.114f, 0f, 0f,
                        0.299f, 0.587f, 0.114f, 0f, 0f,
                        0.299f, 0.587f, 0.114f, 0f, 0f,
                        0f, 0f, 0f, 1f, 0f,
                    )
                )
            )
        }
        canvas.drawBitmap(src, 0f, 0f, paint)
        return out
    }

    private fun scaleDown(src: android.graphics.Bitmap, maxSide: Int): android.graphics.Bitmap {
        val longest = maxOf(src.width, src.height)
        if (longest <= maxSide) return src
        val k = maxSide.toFloat() / longest
        return android.graphics.Bitmap.createScaledBitmap(
            src, (src.width * k).toInt(), (src.height * k).toInt(), true,
        )
    }

    // ── 安全存储（Keystore）─────────────────────────────────────────────
    //
    // 一个本应用的 AES 密钥放在 AndroidKeyStore 里（设备有硬件就落硬件），
    // 条目用它是 GCM 加密后 Base64 存进本应用的 SharedPreferences。这是 Android
    // 给得到的东西，也是为什么 Get 会在 Set 成功过之后仍然失败：删掉锁屏、或把
    // 应用数据恢复到另一台设备，密钥会被作废，它加密过的条目就读不回来了。
    // 那时这里返回 null，Go 侧报 platform.ErrSecretNotFound，并把原因记进 logcat。
    //
    // 与 iOS 的 Keychain 相比少了「按条目设生命周期」：android 的条目设备解锁后
    // 就能读，所以 accessibility 参数只区分「首次解锁后可用」，其余取值等价。

    private fun secretPrefs(ctx: Context) =
        ctx.getSharedPreferences("mygo-secrets", Context.MODE_PRIVATE)

    private fun secretKey(): SecretKey {
        val ks = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
        (ks.getEntry(KEY_ALIAS, null) as? KeyStore.SecretKeyEntry)?.let { return it.secretKey }
        val gen = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore")
        gen.init(
            KeyGenParameterSpec.Builder(
                KEY_ALIAS,
                KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT,
            )
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .setKeySize(256)
                .build()
        )
        return gen.generateKey()
    }

    private fun secretEntryName(ns: String, key: String): String =
        Base64.encodeToString("$ns\u0000$key".toByteArray(Charsets.UTF_8), Base64.NO_WRAP)

    /**
     * 存一个值。返回 true 表示已写入。
     *
     * `accessibility` 目前只用来占位：Keystore 的条目没有按条目的生命周期。
     */
    fun secretSet(ns: ByteArray, key: ByteArray, value: ByteArray, accessibility: Int): Boolean {
        val ctx = activity ?: return false
        return try {
            val cipher = Cipher.getInstance("AES/GCM/NoPadding")
            cipher.init(Cipher.ENCRYPT_MODE, secretKey())
            val sealed = cipher.iv + cipher.doFinal(value)
            secretPrefs(ctx).edit()
                .putString(
                    secretEntryName(String(ns, Charsets.UTF_8), String(key, Charsets.UTF_8)),
                    Base64.encodeToString(sealed, Base64.NO_WRAP),
                )
                .apply()
            true
        } catch (e: Exception) {
            android.util.Log.w("mygo", "secretSet failed: $e")
            false
        }
    }

    /** 读一个值；不存在或密钥已作废时返回 null。 */
    fun secretGet(ns: ByteArray, key: ByteArray): ByteArray? {
        val ctx = activity ?: return null
        val name = secretEntryName(String(ns, Charsets.UTF_8), String(key, Charsets.UTF_8))
        val stored = secretPrefs(ctx).getString(name, null) ?: return null
        return try {
            val sealed = Base64.decode(stored, Base64.NO_WRAP)
            if (sealed.size <= GCM_IV_LEN) return null
            val cipher = Cipher.getInstance("AES/GCM/NoPadding")
            cipher.init(
                Cipher.DECRYPT_MODE,
                secretKey(),
                GCMParameterSpec(GCM_TAG_BITS, sealed, 0, GCM_IV_LEN),
            )
            cipher.doFinal(sealed, GCM_IV_LEN, sealed.size - GCM_IV_LEN)
        } catch (e: Exception) {
            // A key that was invalidated, or a value from another install:
            // unreadable, and worth saying so.
            android.util.Log.w("mygo", "secretGet failed: $e")
            null
        }
    }

    /** 删一个值；不存在也算成功。 */
    fun secretDelete(ns: ByteArray, key: ByteArray): Boolean {
        val ctx = activity ?: return false
        return try {
            secretPrefs(ctx).edit()
                .remove(secretEntryName(String(ns, Charsets.UTF_8), String(key, Charsets.UTF_8)))
                .commit()
        } catch (e: Exception) {
            android.util.Log.w("mygo", "secretDelete failed: $e")
            false
        }
    }
}
/** FCM 的 token 刷新入口；前台消息暂不处理（app 自己决定怎么展示）。 */