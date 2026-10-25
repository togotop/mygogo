#!/usr/bin/env bash
# android-build.sh — 编 Go .so（c-shared）→ 校验 16KB 页对齐 → 若 android/ 在则试 Gradle。
#
# 用法：
#   scripts/android-build.sh [--so-only] [<go-pkg>]
#     <go-pkg>  要编成 .so 的 Go 包。缺省 = $MYGO_ANDROID_PKG = ./internal/android
#               （T2A 负责的安卓 Go 后端包；库名必须是 mygo，对应 System.loadLibrary("mygo")）
#   --so-only   只编 .so + 校验对齐，不碰 Gradle
#
# 环境变量（默认值为实测值，均可覆盖）：
#   ANDROID_HOME / ANDROID_SDK_ROOT   SDK 根（按平台取默认，见脚本内）
#   ANDROID_NDK_VERSION               NDK 目录名（默认 28.2.13676358）
#   MYGO_ANDROID_CC                   C 交叉编译器（默认由 NDK 推导）
#   MYGO_ANDROID_PKG                  默认 Go 包（默认 ./internal/android）
#   MYGO_ANDROID_SO                   输出 .so 路径
#   MYGO_ANDROID_PROXY                Gradle 代理 host:port（如 127.0.0.1:20122）；
#                                    不设在则自动探测 http(s)_proxy 环境变量。
#                                    有代理时注入 Gradle JVM，分发/依赖走代理直连；
#                                    无代理时才走腾讯云分发镜像 + 依赖直连。
#   JAVA_HOME                         Gradle 用的 JDK（默认取平台默认 / java_home）
#
# 平台自适应：HOST_TAG / 可执行后缀由 `uname` 推导，Windows(MSYS) 与 macOS/Linux 共用。
#
# 🔴 16KB 对齐：NDK r28 默认 align 2**14，**不加** CGO_LDFLAGS；本脚本只做校验。
set -u

usage() { sed -n '2,20p' "$0" | sed 's/^# \{0,1\}//'; }

SO_ONLY=0
PKG_ARG=""
for a in "$@"; do
  case "$a" in
    --so-only) SO_ONLY=1 ;;
    -h|--help) usage; exit 0 ;;
    -*) printf '未知参数：%s\n' "$a" >&2; usage; exit 2 ;;
    *)  PKG_ARG="$a" ;;
  esac
done

norm() { printf '%s' "${1//\\//}"; }

# ---- 平台自适应（与 android-doctor.sh 同源）----------------------------------
case "$(uname -s 2>/dev/null)" in
  MINGW*|MSYS*|CYGWIN*)
    HOST_TAG="windows-x86_64"; EXE=".exe"; CLANG_SUFFIX=".cmd"
    DEF_SDK="C:/Users/hzy/AppData/Local/Android/Sdk"; DEF_JDK="C:/Users/hzy/AppData/Local/Programs/Microsoft/jdk-17.0.10.7-hotspot" ;;
  Darwin)
    HOST_TAG="darwin-x86_64"; EXE=""; CLANG_SUFFIX=""
    DEF_SDK="$HOME/Library/Android/sdk"; DEF_JDK="" ;;
  *)
    HOST_TAG="linux-x86_64"; EXE=""; CLANG_SUFFIX=""
    DEF_SDK="$HOME/Android/Sdk"; DEF_JDK="" ;;
esac

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SDK="$(norm "${ANDROID_HOME:-${ANDROID_SDK_ROOT:-$DEF_SDK}}")"
NDK_VER="${ANDROID_NDK_VERSION:-28.2.13676358}"
NDK_DIR="$SDK/ndk/$NDK_VER"
TOOLDIR="$NDK_DIR/toolchains/llvm/prebuilt/$HOST_TAG/bin"
CC="${MYGO_ANDROID_CC:-$TOOLDIR/aarch64-linux-android29-clang$CLANG_SUFFIX}"
OBJDUMP="$TOOLDIR/llvm-objdump$EXE"
# JDK 探测顺序：显式 JAVA_HOME → /usr/libexec/java_home → homebrew keg-only（21/17 版）。
if [ -z "${JAVA_HOME:-}" ]; then
  if [ -x /usr/libexec/java_home ]; then
    DEF_JDK="$(/usr/libexec/java_home 2>/dev/null || true)"
  fi
  if [ -z "$DEF_JDK" ]; then
    for cand in /opt/homebrew/opt/openjdk@21 /opt/homebrew/opt/openjdk@17 /opt/homebrew/opt/openjdk; do
      [ -x "$cand/bin/java" ] && { DEF_JDK="$cand"; break; }
    done
  fi
fi
JDK="$(norm "${JAVA_HOME:-$DEF_JDK}")"

PKG="${PKG_ARG:-${MYGO_ANDROID_PKG:-./internal/android}}"

# ---- HarfBuzz / FreeType 静态库 ---------------------------------------------
# 安卓的字体整形走 C HarfBuzz（shaping/chb.go），链接的是预编译静态库。
# 它们不进 git（hb-build/.gitignore 挡住 src/），要么本机跑过 build.sh，
# 要么 CI 从 release 下载解开——两条路都落在同一个位置。
HB_ROOT="$REPO/third_party/hb-build/lib/android-arm64"
if [ -z "${MYGO_HB_ANDROID:-}" ]; then
  if [ -f "$HB_ROOT/lib/libharfbuzz.a" ] && [ -f "$HB_ROOT/lib/libfreetype.a" ]; then
    MYGO_HB_ANDROID="$HB_ROOT"
  fi
fi
if [ -z "${MYGO_HB_ANDROID:-}" ]; then
  printf '\360\237\224\264 \346\211\276\344\270\215\345\210\260 HarfBuzz \351\235\231\346\200\201\345\272\223\n'
  printf '   \351\242\204\346\234\237\357\274\232%s/lib/libharfbuzz.a\n' "$HB_ROOT"
  printf '   \342\206\263 \344\272\214\351\200\211\344\270\200\357\274\232\n'
  printf '     a) \346\234\254\346\234\272\347\274\226\357\274\232bash third_party/hb-build/build.sh android\n'
  printf '     b) \344\270\213\350\275\275\357\274\232curl -fsSL https://github.com/Heyana/mygo-upstream/releases/download/hb-android-arm64/hb-android-arm64.tar.gz | tar xz -C third_party/hb-build/src/lib\n'
  exit 2
fi
# chb.go 的 #cgo 用 ${SRCDIR}/../hb-build 找库，那在 module cache 里不成立；
# 这几个标志把真实路径塞进去（文件注释里也是这么写的用法）。
export CGO_CFLAGS="${CGO_CFLAGS:-} -I$MYGO_HB_ANDROID/include/harfbuzz -I$MYGO_HB_ANDROID/include/freetype2"
export CGO_LDFLAGS="${CGO_LDFLAGS:-} -L$MYGO_HB_ANDROID/lib -lharfbuzz -lfreetype -lm -llog"

# ---- 输出路径 ---------------------------------------------------------------
if [ -n "${MYGO_ANDROID_SO:-}" ]; then
  SO="$MYGO_ANDROID_SO"
else
  # 直接产出到 jniLibs：AGP 只从 app/src/main/jniLibs/<abi>/ 打包，写到别处
  # 的话 Gradle 看着 BUILD SUCCESSFUL，而 APK 里没有这个 .so。目录自己建，
  # 因为新 clone 里没有它（.so 是构建产物，已被 .gitignore）。
  SO="$REPO/android/app/src/main/jniLibs/arm64-v8a/libmygo.so"
fi
mkdir -p "$(dirname "$SO")"

# ---- 前置检查 ---------------------------------------------------------------
if [ ! -f "$CC" ]; then
  printf '\360\237\224\264 找不到 CC：%s\n' "$CC"
  printf '   \342\206\263 \350\241\245\357\274\232先跑 scripts/android-doctor.sh，或设 MYGO_ANDROID_CC / ANDROID_NDK_VERSION\n'
  exit 2
fi

PKGDIR="$REPO/${PKG#./}"
if [ ! -d "$PKGDIR" ]; then
  if [ "$PKG" = "./internal/android" ]; then
    printf '\342\232\240\357\270\217  %s 不存在（T2A 的安卓 Go 后端包尚未落地）。\n' "$PKG"
    printf '     回退到 smoke 包 ./cmd/mygo —— 仅用于验证 .so 管线，**不是**最终安卓库。\n'
    printf '     待 T2A 落地后本脚本会自动优先编 %s。\n' "$PKG"
    PKG="./cmd/mygo"
    PKGDIR="$REPO/${PKG#./}"
  else
    printf '\360\237\224\264 找不到 Go 包 %s（%s 不存在）\n' "$PKG" "$PKGDIR"
    printf '   \342\206\263 \350\241\245\357\274\232用 MYGO_ANDROID_PKG 或位置参数指定正确的包\n'
    exit 2
  fi
fi

mkdir -p "$(dirname "$SO")"

# ---- 1) 编 .so --------------------------------------------------------------
printf '\n== 编译 %s -> %s ==\n' "$PKG" "$SO"
printf '   GOOS=android GOARCH=arm64 CGO_ENABLED=1 CC=%s\n' "$CC"
printf '   go build -buildmode=c-shared -o %s %s\n' "$SO" "$PKG"

( cd "$REPO" && GOOS=android GOARCH=arm64 CGO_ENABLED=1 CC="$CC" \
    go build -buildmode=c-shared -o "$SO" "$PKG" )
rc=$?
if [ "$rc" -ne 0 ]; then
  printf '\360\237\224\264 go build 失败（exit=%d）\n' "$rc"
  printf '   \342\206\263 \350\241\245\357\274\232对照上面的编译器报错；常见原因 = 包不存在/不是 main 包/缺少 #cgo 头文件\n'
  exit "$rc"
fi
printf '   \342\234\205 已产出：%s（%s 字节）\n' "$SO" "$(wc -c < "$SO" | tr -d ' ')"

# ---- 2) 16KB 页对齐校验 -----------------------------------------------------
printf '\n== 16KB 页对齐校验（期望每条 LOAD 段 align 2**14）==\n'
if [ ! -f "$OBJDUMP" ]; then
  printf '\360\237\224\264 找不到 llvm-objdump：%s\n' "$OBJDUMP"
  printf '   \342\206\263 \350\241\245\357\274\232确认 NDK r28 已装\n'
  exit 2
fi

loads="$("$OBJDUMP" -p "$SO" 2>/dev/null | grep -w LOAD)"
if [ -z "$loads" ]; then
  printf '\360\237\224\264 objdump 未读到任何 LOAD 段（文件损坏？）\n'
  exit 1
fi

align_bad=0
while IFS= read -r line; do
  printf '   %s\n' "$line"
  case "$line" in
    *"align 2**14"*) : ;;
    *) align_bad=1 ;;
  esac
done <<< "$loads"

if [ "$align_bad" -eq 0 ]; then
  printf '   \342\234\205 全部 LOAD 段 align 2**14（16KB 页对齐 OK）\n'
else
  printf '\360\237\224\264 存在 align 不是 2**14 的 LOAD 段 —— 不满足 16KB 页要求\n'
  printf '   \342\206\263 \350\241\245\357\274\232确认用的是 NDK r28（r28 默认即 2**14）\n'
  exit 1
fi

# ---- 3) Gradle（可选）-------------------------------------------------------
if [ "$SO_ONLY" -eq 1 ]; then
  printf '\n== --so-only：跳过 Gradle ==\n'
  exit 0
fi

printf '\n== Gradle 构建（可选）==\n'
if [ ! -d "$REPO/android" ]; then
  printf '   \342\232\240\357\270\217  mygo/android/ 不存在（T2B 负责的宿主工程尚未落地）→ 跳过 APK 构建。\n'
  printf '      .so 已就绪；待 android/ 落地后，本脚本会自动尝试 gradlew assembleDebug。\n'
  exit 0
fi

GRADLE=""
if [ -x "$REPO/android/gradlew" ]; then
  GRADLE="$REPO/android/gradlew"
elif command -v gradle >/dev/null 2>&1; then
  GRADLE="gradle"
fi

if [ -z "$GRADLE" ]; then
  printf '   \342\232\240\357\270\217  没找到 gradle / android/gradlew → 跳过 APK 构建（不是错误）。\n'
  printf '      \342\206\263 \350\241\245\357\274\232安装 Gradle 并加进 PATH，或由 T2B 提供 android/gradlew。\n'
  exit 0
fi

if [ ! -f "$JDK/bin/java" ]; then
  printf '   \342\232\240\357\270\217  找不到 JDK（%s/bin/java）→ Gradle 可能失败。\n' "$JDK"
  printf '      \342\206\263 \350\241\245\357\274\232设 JAVA_HOME 指向 JDK 17。\n'
fi

# ---- 代理自动探测 -----------------------------------------------------------
# AGP/Gradle 的 JVM 默认不读 shell 的 http_proxy 环境变量，必须显式注入
# systemProp.*（或 GRADLE_OPTS 的 -Dhttp(s).proxy*）才能走代理。这里探测常见
# 代理变量，或用 MYGO_ANDROID_PROXY 直接指定 host:port；探测到就注入 Gradle JVM，
# 此时分发与依赖都经代理直连官方源即可，不再走腾讯镜像。无代理时才走腾讯
# 云分发镜像 + 依赖直连（见下段）。
PROXY_HOST=""; PROXY_PORT=""
if [ -n "${MYGO_ANDROID_PROXY:-}" ]; then
  _p="${MYGO_ANDROID_PROXY#*://}"; _p="${_p#*@}"
  PROXY_HOST="${_p%%:*}"; PROXY_PORT="${_p##*:}"
elif [ -n "${https_proxy:-${HTTPS_PROXY:-${http_proxy:-${HTTP_PROXY:-}}}}" ]; then
  _p="${https_proxy:-${HTTPS_PROXY:-${http_proxy:-$HTTP_PROXY}}}"
  _p="${_p#*://}"; _p="${_p#*@}"; _p="${_p%%/*}"   # 去 scheme / 认证 / 路径，只留 host:port
  PROXY_HOST="${_p%%:*}"
  PROXY_PORT="${_p##*:}"
fi
if [ -n "$PROXY_HOST" ] && [ -n "$PROXY_PORT" ] && [ "$PROXY_HOST" != "$PROXY_PORT" ]; then
  export GRADLE_OPTS="${GRADLE_OPTS:-} -Dhttp.proxyHost=$PROXY_HOST -Dhttp.proxyPort=$PROXY_PORT -Dhttps.proxyHost=$PROXY_HOST -Dhttps.proxyPort=$PROXY_PORT -Dhttp.nonProxyHosts=localhost"
  PROXY_ACTIVE=1
  printf '   检测到代理 %s:%s，已注入 Gradle JVM（分发/依赖走代理，不分发镜像）。\n' "$PROXY_HOST" "$PROXY_PORT"
else
  PROXY_ACTIVE=0
fi

# ---- Gradle 分发镜像：腾讯云优先，失败回退国际源 ---------------------------
# gradlew 按 distributionUrl 计算缓存目录并下载分发包；这里临时把 URL 换成
# 腾讯云镜像以加速（国内）。若分发已缓存则跳过换源，避免重复下载；若镜像
# 不可用则换回官方源重试一次。脚本退出（trap）时恢复原 gradle-wrapper.properties。
# 仅当未检测到代理时才走镜像；有代理时分发直连官方源（见上段）。
WRAPPER_PROP="$REPO/android/gradle/wrapper/gradle-wrapper.properties"
WRAPPER_BAK=""
DIST_BASE="${GRADLE_USER_HOME:-$HOME/.gradle}/wrapper/dists"
if [ "$PROXY_ACTIVE" -eq 0 ] && [ "$GRADLE" = "$REPO/android/gradlew" ] && [ -f "$WRAPPER_PROP" ]; then
  if ! find "$DIST_BASE" -maxdepth 3 -type d -name 'gradle-[0-9]*' | grep -q .; then
    WRAPPER_BAK="$WRAPPER_PROP.bak"
    cp "$WRAPPER_PROP" "$WRAPPER_BAK"
    URL_LINE="$(grep '^distributionUrl=' "$WRAPPER_PROP")"
    GVER="$(printf '%s' "$URL_LINE" | sed 's/.*gradle-//; s/[-].*//')"
    GTYPE="$(printf '%s' "$URL_LINE" | sed 's/.*gradle-[0-9.]*[-]//; s/\.zip//')"
    GVER="${GVER:-9.1.0}"; GTYPE="${GTYPE:-all}"
    MIRROR_URL="https://mirrors.cloud.tencent.com/gradle/gradle-${GVER}-${GTYPE}.zip"
    DEFAULT_URL="https://services.gradle.org/distributions/gradle-${GVER}-${GTYPE}.zip"
    trap 'if [ -n "$WRAPPER_BAK" ] && [ -f "$WRAPPER_BAK" ]; then cp -f "$WRAPPER_BAK" "$WRAPPER_PROP"; rm -f "$WRAPPER_BAK"; fi' EXIT
    sed -i "s#^distributionUrl=.*#distributionUrl=$MIRROR_URL#" "$WRAPPER_PROP"
    printf '   分发镜像：腾讯云 %s\n' "$MIRROR_URL"
  else
    printf '   分发已缓存，跳过镜像换源。\n'
  fi
else
  if [ "$GRADLE" = "$REPO/android/gradlew" ] && [ -f "$WRAPPER_PROP" ]; then
    printf '   代理已启用，分发保持官方源（经代理下载）。\n'
  fi
fi

GLOG="$REPO/android/.gradle_build.log"
run_gradle() {
  ( cd "$REPO/android" && JAVA_HOME="$JDK" "$GRADLE" assembleDebug ) > "$GLOG" 2>&1
  return $?
}

printf '   使用：%s（JAVA_HOME=%s）\n' "$GRADLE" "$JDK"
if run_gradle; then
  grc=0
else
  grc=$?
  if [ -n "$WRAPPER_BAK" ] && [ -f "$WRAPPER_BAK" ] && grep -qiE 'download|connect|unknown host|received fatal|timed out|ssl|resolve|unable to' "$GLOG"; then
    printf '   [warn] 腾讯云镜像不可用，回退官方源重试。\n'
    cp -f "$WRAPPER_BAK" "$WRAPPER_PROP"
    sed -i "s#^distributionUrl=.*#distributionUrl=$DEFAULT_URL#" "$WRAPPER_PROP"
    if run_gradle; then grc=0; else grc=$?; fi
  fi
fi

if [ "$grc" -ne 0 ]; then
  printf 'Gradle 失败（exit=%d）—— 详见 %s（不是 Go 侧问题）\n' "$grc" "$GLOG"
  exit "$grc"
fi
printf '   Gradle assembleDebug 完成\n'
