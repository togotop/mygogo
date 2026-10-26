#!/usr/bin/env bash
# android-smoke.sh — 端到端冒烟：按设备 ABI 编 .so → Gradle 安装 → 启动 → 截图与日志。
#
# 用法：
#   scripts/android-smoke.sh [--so-only] [<go-pkg>]
#     <go-pkg>    装进宿主 app 的 Go 包（缺省 ./examples/waveform）
#     --so-only   只编 .so，不装不跑
#
# 环境变量（默认值均可覆盖）：
#   ANDROID_HOME / ANDROID_SDK_ROOT   SDK 根（按平台取默认）
#   ANDROID_NDK_VERSION               NDK 目录名（默认 28.2.13676358）
#   MYGO_SMOKE_DEVICE                 设备序列号（默认：唯一在线设备）
#   MYGO_SMOKE_ABI                    覆盖设备上报的 ABI（arm64-v8a / x86_64）
#   MYGO_SMOKE_WAIT                   启动后等几秒再截图（默认 6）
#   MYGO_SMOKE_OUT                    产物目录（默认 .mygo/android-smoke）
#   MYGO_ANDROID_CC                   覆盖交叉编译器
#   JAVA_HOME                         Gradle 用的 JDK（没设就用 PATH 上的 java；需要 17+）
#
# 与 android-build.sh 的区别：那条按 arm64 编、不碰设备；这条读设备的
# ro.product.cpu.abi，按它编、装、启动，并把截图和 logcat 落到一个目录，
# 所以模拟器（x86_64）和真机（arm64-v8a）走同一条命令。
set -u

usage() { sed -n '2,22p' "$0" | sed 's/^# \{0,1\}//'; }

norm() { printf '%s' "${1//\\//}"; }

SO_ONLY=0
PKG_ARG=""
for a in "$@"; do
  case "$a" in
    --so-only) SO_ONLY=1 ;;
    -h|--help) usage; exit 0 ;;
    -*) printf '未知参数：%s\n' "$a" >&2; usage; exit 2 ;;
    *) PKG_ARG="$a" ;;
  esac
done

# ---- 平台自适应（与 android-build.sh 同源）----------------------------------
case "$(uname -s 2>/dev/null)" in
  MINGW*|MSYS*|CYGWIN*) HOST_TAG="windows-x86_64"; EXE=".exe"; DEF_SDK="C:/Users/hzy/AppData/Local/Android/Sdk" ;;
  Darwin)               HOST_TAG="darwin-x86_64";  EXE="";     DEF_SDK="$HOME/Library/Android/sdk" ;;
  *)                    HOST_TAG="linux-x86_64";   EXE="";     DEF_SDK="$HOME/Android/Sdk" ;;
esac

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SDK="$(norm "${ANDROID_HOME:-${ANDROID_SDK_ROOT:-$DEF_SDK}}")"
ADB="$SDK/platform-tools/adb$EXE"
NDK_VER="${ANDROID_NDK_VERSION:-28.2.13676358}"
TOOLDIR="$SDK/ndk/$NDK_VER/toolchains/llvm/prebuilt/$HOST_TAG/bin"
PKG="${PKG_ARG:-${MYGO_ANDROID_PKG:-./examples/waveform}}"
OUT="${MYGO_SMOKE_OUT:-$REPO/.mygo/android-smoke}"
WAIT="${MYGO_SMOKE_WAIT:-6}"
COMPONENT="${MYGO_ANDROID_COMPONENT:-org.mygo.host/org.mygo.MygoActivity}"

if [ ! -x "$ADB" ]; then
  printf '🔴 找不到 adb：%s\n' "$ADB"
  printf '   → 装 platform-tools，或设 ANDROID_HOME\n'
  exit 2
fi

# ---- 1) 设备与 ABI ----------------------------------------------------------
echo "== [1/5] 设备 =="
if [ -n "${MYGO_SMOKE_DEVICE:-}" ]; then
  DEVICE="$MYGO_SMOKE_DEVICE"
else
  online="$("$ADB" devices | sed '1d' | awk '$2 == "device" {print $1}')"
  n="$(printf '%s\n' "$online" | grep -c . || true)"
  if [ "$n" -eq 0 ]; then
    printf '🔴 无设备/模拟器在线，未跑。\n'
    printf '   → 启动模拟器或插真机开 USB 调试后重跑；多设备时用 MYGO_SMOKE_DEVICE 指定\n'
    exit 1
  fi
  if [ "$n" -gt 1 ]; then
    printf '🔴 有多台设备，用 MYGO_SMOKE_DEVICE 指定一台：\n'
    printf '%s\n' "$online" | sed 's/^/     /'
    exit 2
  fi
  DEVICE="$online"
fi
ABI="${MYGO_SMOKE_ABI:-$("$ADB" -s "$DEVICE" shell getprop ro.product.cpu.abi 2>/dev/null | tr -d '\r')}"
if [ -z "$ABI" ]; then
  printf '🔴 设备 %s 没有回应（adb 状态不是 device）。\n' "$DEVICE"
  printf '   → adb devices 看状态；offline 时 adb reconnect，仍不行就重启模拟器\n'
  exit 1
fi
printf '   ✅ %s，ABI=%s\n' "$DEVICE" "$ABI"

case "$ABI" in
  arm64-v8a) GOARCH=arm64; TRIPLE=aarch64-linux-android29; HB="$REPO/third_party/hb-build/lib/android-arm64" ;;
  x86_64)    GOARCH=amd64; TRIPLE=x86_64-linux-android29;  HB="$REPO/third_party/hb-build/lib/android-x86_64" ;;
  *) printf '🔴 只支持 64 位设备（arm64-v8a / x86_64），这台是 %s\n' "$ABI"; exit 2 ;;
esac

# ---- 2) 交叉编译器与 HarfBuzz ------------------------------------------------
echo "== [2/5] 工具链 =="
CC="${MYGO_ANDROID_CC:-$TOOLDIR/$TRIPLE-clang$EXE}"
[ -f "$CC" ] || CC="$CC.cmd"
if [ ! -f "$CC" ]; then
  printf '🔴 找不到交叉编译器：%s\n' "$CC"
  printf '   → 先跑 scripts/android-doctor.sh，或设 MYGO_ANDROID_CC / ANDROID_NDK_VERSION\n'
  exit 2
fi
if [ ! -f "$HB/lib/libharfbuzz.a" ] || [ ! -f "$HB/lib/libfreetype.a" ]; then
  printf '🔴 找不到 HarfBuzz 静态库：%s/lib\n' "$HB"
  printf '   → bash third_party/hb-build/build.sh android\n'
  exit 2
fi
printf '   ✅ %s\n' "$CC"

# ---- 3) 编 .so --------------------------------------------------------------
SO="$REPO/android/app/src/main/jniLibs/$ABI/libmygo.so"
mkdir -p "$(dirname "$SO")"
echo "== [3/5] 编 $PKG -> $SO =="
( cd "$REPO" && CGO_CFLAGS="-I$HB/include/harfbuzz -I$HB/include/freetype2" \
    CGO_LDFLAGS="-L$HB/lib -lharfbuzz -lfreetype -lm -llog" \
    GOOS=android GOARCH="$GOARCH" CGO_ENABLED=1 CC="$CC" \
    go build -buildmode=c-shared -o "$SO" "$PKG" )
if [ $? -ne 0 ]; then
  printf '🔴 go build 失败\n'
  printf '   → 常见原因：包不是 main、缺 //go:build android 入口、或平台包没有对应实现\n'
  exit 1
fi
printf '   ✅ %s 字节\n' "$(wc -c < "$SO" | tr -d ' ')"

if [ "$SO_ONLY" -eq 1 ]; then
  printf '\n== --so-only：停在编译 ==\n'
  exit 0
fi

# ---- 4) 装 app --------------------------------------------------------------
echo "== [4/5] Gradle installDebug =="
if [ ! -x "$REPO/android/gradlew" ]; then
  printf '🔴 没有 android/gradlew\n'
  exit 2
fi
( cd "$REPO/android" && ./gradlew installDebug --console=plain -q )
if [ $? -ne 0 ]; then
  printf '🔴 Gradle 失败\n'
  printf '   → 需要 JDK 17+（export JAVA_HOME=...）；代理见 scripts/android-build.sh 的 MYGO_ANDROID_PROXY\n'
  exit 1
fi
printf '   ✅ 已安装 %s\n' "$COMPONENT"

# ---- 5) 启动并留证 ----------------------------------------------------------
echo "== [5/5] 启动、截图、日志 =="
mkdir -p "$OUT"
"$ADB" -s "$DEVICE" shell am force-stop "${COMPONENT%%/*}" >/dev/null 2>&1
"$ADB" -s "$DEVICE" logcat -c
"$ADB" -s "$DEVICE" shell am start -n "$COMPONENT" >/dev/null 2>&1 || {
  printf '🔴 启动失败：%s\n' "$COMPONENT"
  printf '   → 用 MYGO_ANDROID_COMPONENT 指定 <applicationId>/<Activity>\n'
  exit 1
}
sleep "$WAIT"
"$ADB" -s "$DEVICE" exec-out screencap -p > "$OUT/shot.png" 2>/dev/null
"$ADB" -s "$DEVICE" logcat -d > "$OUT/logcat.txt" 2>/dev/null
printf '   ✅ 截图 %s（%s 字节）\n' "$OUT/shot.png" "$(wc -c < "$OUT/shot.png" | tr -d ' ')"
printf '   ✅ 日志 %s\n' "$OUT/logcat.txt"

spans="$(grep -E 'plot/(update|paint)|waveform:' "$OUT/logcat.txt" | tail -6)"
if [ -n "$spans" ]; then
  printf '\n== 探针/应用日志（末 6 行）==\n%s\n' "$spans"
else
  printf '\n== 没有探针输出 ==\n'
  printf '   开了 -probe 才有（示例在 Android 上默认开）；否则先看 %s 里的崩溃或报错。\n' "$OUT/logcat.txt"
fi

printf '\n✅ 冒烟走完：%s/%s\n' "$ABI" "$DEVICE"
