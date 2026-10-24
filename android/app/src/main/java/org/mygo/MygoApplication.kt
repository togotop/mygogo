package org.mygo

import android.app.Application
import android.util.Log
import java.io.File

/**
 * 进程级的唯一初始化点，不依赖任何 Activity。
 *
 * 存在的理由：推送、定时任务、开机广播都可能**在没有 Activity 的情况下**把进程
 * 拉起来，那时 MygoActivity.onCreate 根本不会执行。崩溃记录器放在这里，进程
 * 无论被谁拉起、为什么死，上一次的死因都留得下来。
 *
 * 🔴 这里只能做纯 Android 的事：`libmygo.so` 是在 MygoIme / MygoInput 的静态块
 * 里加载的（见 MygoIme.kt），Application 期还没有库，调用 `Native.*` 会直接崩。
 *
 * 诊断页仍由 MygoActivity 显示：它需要一个窗口，而 Application 没有。
 */
class MygoApplication : Application() {

    override fun onCreate() {
        super.onCreate()
        installCrashRecorder()
        startServicesIfHeadless()
    }

    /**
     * 无 Surface 启动：给"窗口归别人所有"的宿主用（游戏引擎自己渲染，MyGo 只做
     * 服务层）。由 `-Pheadless=true` 编成 BuildConfig.MYGO_HEADLESS，默认关，
     * 所以普通宿主的行为一个字不变。
     *
     * 顺序不能反：先给 Go 目录（没有可写目录时路径查询返回空），再装平台桥
     * （否则所有桥调用报 ErrPlatformBridge），最后才启动。
     */
    private fun startServicesIfHeadless() {
        if (!BuildConfig.MYGO_HEADLESS) {
            return
        }
        Native.onPaths(filesDir.absolutePath, cacheDir.absolutePath)
        MygoPlatform.install(this)
        // 宿主出口：真实宿主（Godot 插件）在这里把 JSON 变成引擎信号，这里只记日志。
        MygoPlatform.setHostSink { json -> Log.i("mygo", "hostEvent: " + json) }
        Native.startServices()
        selfTestRequest()
    }

    /**
     * 自检：模拟宿主发起的第一条请求（Godot 里由插件发起）。应用是异步启动的，
     * 处理器要等 run() 注册完才在，所以隔一会儿重试——真实宿主也应等引擎就绪后再发。
     */
    private fun selfTestRequest() {
        Thread {
            val message = "{\"v\":1,\"id\":1,\"cmd\":\"ping\"}"
            repeat(10) { attempt ->
                Thread.sleep(500)
                if (Native.request(message) == 1) {
                    Log.i("mygo", "request accepted after " + (attempt + 1) + " tries")
                    return@Thread
                }
            }
            Log.w("mygo", "request never accepted: is a handler registered?")
        }.start()
    }

    // 内存压力走进程级这一处：Activity 和 Application 都会收到 onTrimMemory，
    // 只转发 Application 的，Go 侧就不会为一次事件收到两遍。库只在启用无 Surface
    // 启动后才确定加载过，所以这里吞掉"库还没加载"的异常。
    override fun onTrimMemory(level: Int) {
        super.onTrimMemory(level)
        try {
            Native.onTrimMemory(level)
        } catch (t: Throwable) {
            Log.w("mygo", "onTrimMemory ignored: " + t)
        }
    }

    // crashFile is where the uncaught handler leaves the reason, in the app's
    // own directory. The Go side writes a panic to the same file
    // (see android_start.go: files/<app name> means the file sits one level up).
    private fun crashFile() = File(filesDir, "crash.txt")

    // installCrashRecorder writes an uncaught exception to crashFile before the
    // process dies, and lets the system's own handler go on doing what it did.
    private fun installCrashRecorder() {
        val previous = Thread.getDefaultUncaughtExceptionHandler()
        Thread.setDefaultUncaughtExceptionHandler { thread, error ->
            try {
                crashFile().writeText(Log.getStackTraceString(error))
            } catch (ignored: Throwable) {
            }
            previous?.uncaughtException(thread, error)
        }
    }

    // takeLastCrash returns what the last run wrote, and takes it away so the
    // screen shows once rather than on every launch. A Go panic lands in the
    // same file, written by android_start.go.
    fun takeLastCrash(): String? {
        val file = crashFile()
        if (!file.exists()) {
            return null
        }
        val text = try {
            file.readText()
        } catch (ignored: Throwable) {
            return null
        }
        file.delete()
        return "The app stopped on the last run.\n\n" + text
    }
}
