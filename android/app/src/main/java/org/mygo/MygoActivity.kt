package org.mygo

import android.app.Activity
import android.content.Intent
import android.content.pm.PackageManager
import android.graphics.Color
import android.os.Build
import android.os.Bundle
import android.os.SystemClock
import android.util.Log
import android.view.KeyEvent
import android.util.TypedValue
import android.view.Window
import android.view.WindowManager
import android.widget.ScrollView
import android.widget.TextView
import android.widget.Toast
import android.window.OnBackInvokedDispatcher

/**
 * 宿主 Activity：只做「挂一个 MygoView 并保持前台」。
 *
 * 设计要点：
 *  · 不在 onDestroy 里通知 Go 侧释放 —— Go 侧保活（见 MygoView 文件头注释 #1）。
 *    语义上：「Activity 没了」不等于「Go 侧渲染上下文没了」。
 *  · 不做任何会改动 SurfaceView z-order 语义的层级加工（保持 setContentView(view) 单层）。
 *  · **系统栏显示**（状态栏 + 手势条），内容铺到屏幕边缘再由 Go 侧躲开 ——
 *    主题见 AndroidManifest.xml 的 MygoTheme，躲开的机制见 MygoView.reportSafeArea。
 *  · 屏幕方向策略见 AndroidManifest.xml 的注释（允许旋转 + configChanges）。
 */
class MygoActivity : Activity() {

    private lateinit var view: MygoView

    // 首页返回键第一次按下的时刻；连续两次才离开应用，见 goBack。
    private var lastBackAt = 0L

    // backToExit 是「Go 没接管返回时，宿主怎么离开」的策略，由清单的
    // <meta-data android:name="mygo.backToExit" android:value="..."/> 选择：
    //   confirm（默认）  第一次只提示，2 秒内第二次才离开
    //   immediate        直接离开
    //   none             宿主不离开，返回完全交给应用
    // 没写或写了别的值都按 confirm —— 与加入这个开关之前的行为一致。
    private val backToExit: String by lazy {
        try {
            val info = packageManager.getApplicationInfo(packageName, PackageManager.GET_META_DATA)
            info.metaData?.getString("mygo.backToExit") ?: "confirm"
        } catch (t: Throwable) {
            "confirm"
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        // 闪崩后第二次打开要能看到原因：没有 adb 的设备上，崩溃只留下一个
        // 消失的窗口。handler 装在 MygoApplication（进程级，推送拉起的进程也
        // 算），这里只负责显示，读完即删 —— 只显示这一次。
        (application as? MygoApplication)?.takeLastCrash()?.let {
            showDiagnosis(it)
            return
        }

        // 用户在系统设置里调大字体：只有文字跟着放大，间距与控件尺寸不变。
        // 字体大小变化不在 configChanges 里，系统会重建 Activity，所以这里读到
        // 的就是新值；Go 侧下次布局用新值重排所有文字。
        Native.onFontScale(resources.configuration.fontScale)

        // ActionBar 不要：本 app 自己画顶栏（lab/shell.go）。主题里也写了
        // NoActionBar，这里是同一件事的代码侧 —— 两处都留是因为主题可以被
        // 换掉，而这行不会。
        requestWindowFeature(Window.FEATURE_NO_TITLE)
        // 渲染场景常亮；避免用户触摸时息屏把 surface 带走。
        window.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)

        // 🔴 这里**不**加 FLAG_FULLSCREEN，也不碰 systemUiVisibility：
        // 这是个普通 app，状态栏和手势条都要显示（主题见 AndroidManifest.xml
        // 的 MygoTheme）。要隐藏系统栏的是全屏类 app（播放器、游戏），
        // 那应当由那个 Activity 自己声明，而不是这个宿主默认替所有 app 决定。
        //
        // 🔴 layoutInDisplayCutoutMode 在**代码**里设，不在主题里：
        // 它不是 style 属性，是 WindowManager.LayoutParams 的一个字段 —— 写成
        // `<item name="android:layoutInDisplayCutoutMode">` 会让 aapt 报
        // "style attribute not found"（实测），因为那个名字在 attr 表里只有
        // LayoutParams 用得到。
        //
        // SHORT_EDGES = 允许内容画到短边之外的刘海区域，即"背景铺到屏幕边缘"。
        // 这正是「背景铺满 + 内容躲开」要的那一半：这里允许画过去，躲多少由
        // Go 侧的 SafeArea 决定（见 MygoView.reportSafeArea）。默认的
        // DEFAULT 会在有刘海的设备上把窗口整个裁到刘海以下 —— 背景也铺不过去，
        // 屏幕顶部会出现一条与 app 无关的颜色带。
        //
        // 有刘海才有区别。本机（PKJ110）是挖孔屏：dumpsys 报
        // DisplayCutout insets=Rect(0,160-0,0)，即顶部 160px（40 DIP）的挖孔区，
        // 与状态栏同高，所以两种模式在本机看着一样。
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {
            window.attributes = window.attributes.apply {
                layoutInDisplayCutoutMode =
                    WindowManager.LayoutParams.LAYOUT_IN_DISPLAY_CUTOUT_MODE_SHORT_EDGES
            }
        }

        // 🔴 让窗口自己处理 insets。targetSdk 36 强制 edge-to-edge，但如果不
        // 明确说不 fit，平台仍按旧行为把内容挤开 —— 而在这个过程中
        // `WindowInsets.Type.ime()` 不会派发到 View：键盘"已显示"
        // （mInputShown=true、IME 窗口有 surface），insets 里却始终没有它，
        // 于是没有任何东西让位，键盘被压在内容下面看不见。
        //
        // false = 内容画到系统栏与键盘下面，各让多少由 Go 侧的布局按
        // MygoView.reportSafeArea 报上来的值做 —— 与本文件上面
        // layoutInDisplayCutoutMode 的分工一致：这里只允许画过去，躲多少在 Go。
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
            window.setDecorFitsSystemWindows(false)
        }

        // The app's own directories, which are the only ones it can write:
        // an Android app has no home directory, so mygo's paths come from
        // these.
        Native.onPaths(filesDir.absolutePath, cacheDir.absolutePath)
        // 首帧之前 surface 用什么颜色：主题自己的 windowBackground，启动才是
        // 从同一块底色接上来，而不是先闪一下系统底色。
        themedBackground()?.let { Native.onBackground(it) }

        view = MygoView(this)
        // 单层 View 层级：SurfaceView 直接作为 content view。
        setContentView(view)

        // Android 13 sends the back key to a callback instead of onBackPressed.
        // 🔴 报了 33 的兼容层不一定真有这个 dispatcher（鸿蒙 3.0 就报 31 而
        // 没有 WindowInsets.Type 的任何方法）。调用失败就不注册回调，按键照旧
        // 走下面的 onBackPressed，做的是同一件事。
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            try {
                onBackInvokedDispatcher.registerOnBackInvokedCallback(
                    OnBackInvokedDispatcher.PRIORITY_DEFAULT,
                ) { goBack() }
            } catch (t: Throwable) {
                Log.w("mygo", "onBackInvokedDispatcher 不可用，返回键留给 onBackPressed: $t")
            }
        }

        // 让 View 拿到焦点，否则 onKeyDown/onKeyUp 不会派发到它。
        view.grabInputFocus()

        // 🔴 图片服务地址不在这里传了。
        //
        // 以前是 gradle -PmygoLabList → BuildConfig.LAB_LIST → Os.setenv →
        // Go 的 os.Getenv：把编译期常量绕两个运行时弯交给 Go，还带一个
        // 「setenv 必须早于 loadLibrary」的时序雷（Go 在包 init 里读，晚一步
        // 就静默为空）。
        //
        // 现在它由 lab 自己的构建脚本编进 .so：
        //   MYGO_LAB_LIST=http://<主机>:8098 bash build.sh android
        // 见 mygo-wind-router-lab/lab/listbase_android.go。地址随网络变、
        // 不随运行变，而那个 .so 本来就是「为拿 adb 的这台机器」编的。

        // 平台能力（剪贴板/URL/通知/无障碍）的 Java 侧握手；必须在任何平台能力被使用前调用。
        MygoPlatform.install(this, view)

        // 冷启动时，URL 在 onCreate 的 intent 里；Go 侧会缓冲到 Init 装好投递口，
        // 所以这里早于 Go 就绪也不会丢。
        intent?.let { deliverDeepLink(it) }
    }

    /**
     * 把系统应用的返回结果转给平台侧：扫码就是从这里回到 Go 的。
     *
     * 用旧 API 而不是 registerForActivityResult：这里只转发，不关心生命周期，
     * 而旧 API 在 `Activity`（非 ComponentActivity）上不需要额外依赖。
     */
    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        super.onActivityResult(requestCode, resultCode, data)
        MygoPlatform.onScanResult(requestCode, resultCode, data)
        MygoPlatform.onMediaResult(requestCode, resultCode, data)
    }

    /**
     * 授权对话框的结果转给平台侧。
     *
     * 与 onActivityResult 同理用旧 API：这里只转发，不关心生命周期，也不需要
     * ComponentActivity 的额外依赖。
     */
    override fun onRequestPermissionsResult(requestCode: Int, permissions: Array<String>, grantResults: IntArray) {
        super.onRequestPermissionsResult(requestCode, permissions, grantResults)
        MygoPlatform.onPermissionResult(requestCode, grantResults)
    }

    /**
     * 应用已在运行时，URL 从 onNewIntent 进来（launchMode=singleTask）。
     * 同步 setIntent，之后 getIntent() 拿到的也是这一条。
     */
    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        deliverDeepLink(intent)
    }

    /**
     * 深链的两个来源：Intent 的 data（标准的 ACTION_VIEW），以及 mygo.url
     * extra。后者是给推送厂商 SDK 用的 —— 它们把点击通知做成启动主
     * Activity 的 Intent，不一定设 data，但可以带 extra。两条都交给
     * OnOpenURL，冷启动与后台点击走同一条路。
     */
    private fun deliverDeepLink(intent: Intent) {
        val url = intent.data?.toString()?.takeIf { it.isNotEmpty() }
            ?: intent.getStringExtra(EXTRA_DEEP_LINK)?.takeIf { it.isNotEmpty() }
            ?: return
        MygoPlatform.deliverOpenURL(url)
    }

    // nativeCall 让「Go 库缺失」不会第二次带走屏幕：诊断页本来就是给那种
    // 设备看的，而一个会抛的生命周期调用会用自己的崩溃覆盖掉原因。
    private fun nativeCall(what: String, call: () -> Unit) {
        try {
            call()
        } catch (t: Throwable) {
            Log.w("mygo", what + " failed: " + t)
        }
    }

    override fun onStart() {
        super.onStart()
        nativeCall("onStart") { Native.onLifecycle(2) }
    }

    override fun onResume() {
        super.onResume()
        // 回到前台后焦点可能被系统收走，重新要一次；这里**不**做任何 surface 生命周期动作。
        // 🔴 view 可能不存在：启动失败或显示上次崩溃时，屏幕上没有 MygoView，
        // 碰未初始化的 lateinit 会用自己的崩溃盖掉那一屏原因。
        if (::view.isInitialized) {
            view.grabInputFocus()
        }
        nativeCall("onResume") { Native.onLifecycle(0) }
    }

    override fun onPause() {
        nativeCall("onPause") { Native.onLifecycle(1) }
        super.onPause()
    }

    override fun onStop() {
        // 离开前台后第一次返回的计时不再算数，回来要重新按两次。
        lastBackAt = 0
        nativeCall("onStop") { Native.onLifecycle(3) }
        super.onStop()
    }

    // 返回键有两条到达路径：API 33+ 的 OnBackInvokedCallback（手势导航），
    // 以及更老的框架把 KEYCODE_BACK 派给窗口（鸿蒙 3.0 报 API 31；4.x 的兼容层
    // 也可能如此）。后者先落到有焦点的 MygoView，被它当输入吞掉 —— 只靠
    // onBackPressed 在这些设备上永远等不到，表现就是「返回完全没反应」。
    // Activity.dispatchKeyEvent 在视图层级之前调用，从这里截住才可靠。
    override fun dispatchKeyEvent(event: KeyEvent): Boolean {
        if (event.keyCode == KeyEvent.KEYCODE_BACK) {
            if (event.action == KeyEvent.ACTION_UP && !event.isCanceled) {
                goBack()
            }
            return true
        }
        return super.dispatchKeyEvent(event)
    }

    // Before Android 13, the back key arrives here.
    @Suppress("DEPRECATION")
    override fun onBackPressed() {
        goBack()
    }

    // themedBackground reads the window background the theme sets, as ARGB, or
    // null when it is not a plain colour (a drawable the Go side cannot use).
    private fun themedBackground(): Int? {
        val value = TypedValue()
        if (!theme.resolveAttribute(android.R.attr.windowBackground, value, true)) {
            return null
        }
        if (value.type < TypedValue.TYPE_FIRST_COLOR_INT || value.type > TypedValue.TYPE_LAST_COLOR_INT) {
            return null
        }
        return value.data
    }

    // showDiagnosis puts the last run's reason on the screen. Dark on purpose,
    // so it reads the same in either appearance, and selectable so a phone
    // with no adb can still copy it out.
    private fun showDiagnosis(message: String) {
        val text = TextView(this).apply {
            this.text = message
            setTextIsSelectable(true)
            setPadding(48, 120, 48, 48)
            textSize = 13f
            setTextColor(Color.WHITE)
            setBackgroundColor(Color.rgb(17, 19, 21))
        }
        setContentView(ScrollView(this).apply { addView(text) })
    }

    /**
     * The back gesture leaves a page, and the app once no page is open. The
     * app answers through Native.back; it leaves the app when it does not.
     */
    private fun goBack() {
        // back 走 JNI 到 Go；库没加载好时它也可能抛，别让返回把应用带走。
        var handled = false
        nativeCall("back") { handled = Native.back() }
        if (handled) {
            return
        }
        when (backToExit) {
            // "none"：应用自己决定返回做什么，宿主不离开。Go 没接管时按了也没反应。
            "none" -> return
            // "immediate"：不确认，直接离开。
            "immediate" -> {
                finish()
                return
            }
        }
        // 首页按一次返回只提示，2 秒内再按一次才离开：误触不至于直接退出。
        val now = SystemClock.uptimeMillis()
        // 同一次按键可能同时走 dispatchKeyEvent 与 OnBackInvokedCallback；
        // 150ms 内的第二次调用当重复，不推进「两次退出」的节奏。
        if (now - lastBackAt < 150) {
            return
        }
        if (now - lastBackAt <= 2000) {
            finish()
            return
        }
        lastBackAt = now
        Toast.makeText(this, "再按一次退出", Toast.LENGTH_SHORT).show()
    }

    private companion object {
        /** 推送 SDK 的点击 Intent 约定字段：里面放深链。 */
        const val EXTRA_DEEP_LINK = "mygo.url"
    }

    // 有意不重写 onDestroy 去释放 Go 侧状态：Go 侧负责保活。
    // 若将来需要「App 真正退出才释放」，应在 Application/进程级做，而不是 Activity 级。
}
