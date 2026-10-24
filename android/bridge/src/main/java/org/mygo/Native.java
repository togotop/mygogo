package org.mygo;

import android.view.Surface;

import java.nio.charset.StandardCharsets;

/**
 * JNI 契约类（与 Go 侧逐字对齐）。
 *
 * <p><b>为什么用 Java 而不是 Kotlin 写这个类</b>（任务书允许，且这是有意的选择）：
 * <ul>
 *   <li>契约要求的是 <code>public final class Native</code> + <code>static { System.loadLibrary("mygo"); }</code>
 *       + <code>private static native ...</code>。</li>
 *   <li>若用 Kotlin 写，这些成员只能放进 <code>companion object</code>。不带
 *       <code>@JvmStatic</code> 时，native 方法实际挂在 <code>Native$Companion</code> 上，
 *       生成的 JNI 符号会变成 <code>Java_org_mygo_Native_00024Companion_nativeOnTouch</code>，
 *       而不是 Go 侧按契约期望的 <code>Java_org_mygo_Native_nativeOnTouch</code>。</li>
 *   <li>Java 的 <code>private static native</code> 生成的符号与契约字面一一对应，零推断风险。</li>
 * </ul>
 *
 * <p>库名固定为 <code>"mygo"</code> ⇒ 运行时加载 <code>libmygo.so</code>，
 * 由 Go 侧产出后放进 <code>app/src/main/jniLibs/&lt;abi&gt;/libmygo.so</code>。
 *
 * <p>⚠️ 下面 7 个 native 方法的**名字与签名不许改**（Go 侧按同一份契约实现）。
 * 新增方法是允许的（见 {@link #nativeOnSafeArea}）：这条约束说的是**已有的**
 * 不许单方面改，不是不许加 —— 加一个只在两边同时加才编得过，不会静默失配。
 */
public final class Native {

    static {
        // 库名必须逐字是 "mygo"
        System.loadLibrary("mygo");
    }

    private Native() {
        // 纯静态契约类，禁止实例化
    }

    // ───────────────────────── 契约：7 个 private static native ─────────────────────────
    // 名字/参数逐字对齐 Go 侧，不许改。

    private static native void nativeOnSurfaceCreated(Surface s);

    private static native void nativeOnSurfaceChanged(int w, int h, int dpi);

    /**
     * The user's text size preference (Android's fontScale, 1 by default).
     * Text sizes multiply by it; lengths, paddings and borders do not.
     */
    private static native void nativeOnFontScale(float scale);

    private static native void nativeOnSurfaceDestroyed();

    private static native void nativeOnTouch(int action, float x, float y, long eventTimeMs);

    private static native void nativeOnKey(int keyCode, int action, int metaState);

    private static native void nativeOnSizeChanged(int w, int h);

    /**
     * 系统栏（状态栏、刘海、手势条）压住内容的区域，单位 <b>DIP</b>。
     *
     * <p>本 App 的 theme 是 Fullscreen，内容从屏幕左上角开始画，所以系统栏直接压在
     * 内容上。Go 的布局引擎看不见系统栏，只知道自己的画布多大 —— 这个值只能由这里问
     * 系统再转过去，Go 侧拿它当 padding 用。
     *
     * <p>为什么是 DIP 而不是设备像素：{@code WindowInsets.getInsets()} 给设备像素，
     * Go 的布局按 DIP（与 CSS px 同量级）。换算在这里做，因为 density 只有 Java 侧有；
     * Go 侧拿到的值必须能直接当 padding 用，否则每个调用点都要自己换算一遍。
     */
    private static native void nativeOnSafeArea(int top, int right, int bottom, int left);

    /**
     * The app's private directories, which are the only ones it can write: an
     * Android app has no home directory. They arrive once, before the app
     * starts, and become mygo's user data, cache and log paths.
     */
    private static native void nativeOnPaths(byte[] data, byte[] cache);

    /**
     * 在没有 Surface 的情况下启动 Go 应用。
     *
     * <p>给"窗口归别人所有"的宿主用：游戏引擎自己渲染，MyGo 只做服务层。应用照常跑，
     * 但不能建窗口（{@code mygo.WindowsSupported()} 返回 false）。
     *
     * <p>调用前必须先 {@code nativeOnPaths}，否则 Go 侧没有可写目录；平台能力也要先
     * {@code MygoPlatform.install(context)}，否则所有桥调用都会报 ErrPlatformBridge。
     *
     * <p>和 Surface 路径不冲突：先到的那条消费启动钩子，应用只会启动一次。
     */
    private static native void nativeStartServices();

    /**
     * 一条 JSON 请求交给 Go 服务层。1 = 已受理，0 = 拒绝（没注册处理器或队列满）。
     * 只入队，不在调用线程执行；结果由 Go 侧经 {@code MygoPlatform.hostEvent} 推回。
     */
    private static native int nativeRequest(byte[] json);

    /**
     * The theme's window background as ARGB. The surface is painted with it
     * until the content's first frame is ready, so a start does not flash a
     * colour the app never shows.
     */
    private static native void nativeOnBackground(int argb);

    /**
     * Where the Activity is now: 0 resumed, 1 paused, 2 started, 3 stopped.
     * Go hears it as the lifecycle events every platform reports.
     */
    private static native void nativeOnLifecycle(int state);

    /**
     * Android's memory pressure, as {@code onTrimMemory} reports it.
     */
    private static native void nativeOnTrimMemory(int level);

    /**
     * The system's back gesture, answered before it leaves the app. Returns
     * whether the app went back a page; the host leaves the app when it did
     * not.
     */
    private static native boolean nativeBack();

    // ───────────────────────── 包内包装方法（可加，不涉及契约名）─────────────────────────
    // native 方法是 private，同包的 View 需要一层包内可见的转发入口。
    // 包装方法只做转发，不改变任何语义。

    static void onSurfaceCreated(Surface s) {
        nativeOnSurfaceCreated(s);
    }

    static void onSurfaceChanged(int w, int h, int dpi) {
        nativeOnSurfaceChanged(w, h, dpi);
    }

    static void onFontScale(float scale) {
        nativeOnFontScale(scale);
    }

    static void onSurfaceDestroyed() {
        nativeOnSurfaceDestroyed();
    }

    static void onTouch(int action, float x, float y, long eventTimeMs) {
        nativeOnTouch(action, x, y, eventTimeMs);
    }

    static void onKey(int keyCode, int action, int metaState) {
        nativeOnKey(keyCode, action, metaState);
    }

    static void onSizeChanged(int w, int h) {
        nativeOnSizeChanged(w, h);
    }

    static void onSafeArea(int top, int right, int bottom, int left) {
        nativeOnSafeArea(top, right, bottom, left);
    }

    /** 没有 Surface 时启动 Go 应用（游戏引擎这类宿主用）。 */
    static void startServices() {
        nativeStartServices();
    }

    /**
     * 把一条 JSON 请求交给 Go 服务层：1 = 已受理，0 = 拒绝（没注册处理器或队列满）。
     *
     * 只入队，不在调用线程上执行——宿主（游戏引擎）的主线程不会被慢请求卡住。
     * 结果不入返回值，由 Go 通过 MygoPlatform.hostEvent 推回来。
     */
    static int request(String json) {
        return nativeRequest(json.getBytes(StandardCharsets.UTF_8));
    }

    static void onPaths(String data, String cache) {
        nativeOnPaths(data.getBytes(StandardCharsets.UTF_8), cache.getBytes(StandardCharsets.UTF_8));
    }

    static void onBackground(int argb) {
        nativeOnBackground(argb);
    }

    static void onTrimMemory(int level) {
        nativeOnTrimMemory(level);
    }

    static void onLifecycle(int state) {
        nativeOnLifecycle(state);
    }

    static boolean back() {
        return nativeBack();
    }
}
