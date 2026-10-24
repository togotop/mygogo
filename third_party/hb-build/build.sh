#!/bin/bash
# 构建 FreeType + HarfBuzz 静态库（macOS 本机 / Android arm64 / Android x86_64）
#
# 用法: bash build.sh [macos|android|android-x86_64]
#
#   macos           本机静态库
#   android         Android arm64-v8a（也接受 android-arm64）
#   android-x86_64  Android x86_64
#
# 产出在 lib/<abi>/{include,lib}（abi = macos / arm64 / x86_64），这些是构建
# 产物，被 .gitignore 挡住。想给别人用（CI）就把 lib/<abi> 打成 tar.gz 挂到
# release 上——scripts/android-build.sh 会从那里下载解开。
set -euo pipefail
cd "$(dirname "$0")"

TARGET="${1:-macos}"
ROOT="$(pwd)"
SRC=src

# ABI 选择：输出目录 tag、交叉编译器三元组、meson 交叉文件模板。
MESON_INI=""
case "$TARGET" in
  macos)
    ABI_TAG=macos
    CC="clang -arch arm64"
    FT_CONF="--host=aarch64-apple-darwin"
    ;;
  android | android-arm64)
    ABI_TAG=arm64
    CLANG_TRIPLE=aarch64-linux-android
    MESON_INI=android-arm64.ini
    FT_CONF="--host=aarch64-linux-android"
    ;;
  android-x86_64)
    ABI_TAG=x86_64
    CLANG_TRIPLE=x86_64-linux-android
    MESON_INI=android-x86_64.ini
    FT_CONF="--host=x86_64-linux-android"
    ;;
  *)
    echo "未知 target: $TARGET（可选 macos / android / android-x86_64）" >&2
    exit 2
    ;;
esac

if [ "$ABI_TAG" = macos ]; then OUT=lib/macos; else OUT="lib/android-$ABI_TAG"; fi
mkdir -p "$OUT"

if [ "$ABI_TAG" != macos ]; then
  # NDK 位置：显式变量优先，其次 ANDROID_HOME/ANDROID_SDK_ROOT，最后按平台猜。
  # 不写死某一台的路径——CI 上的 SDK 在 /usr/local/lib/android/sdk。
  if [ -z "${NDK:-}" ]; then
    SDK="${ANDROID_HOME:-${ANDROID_SDK_ROOT:-}}"
    if [ -z "$SDK" ]; then
      case "$(uname -s)" in
        Darwin) SDK="$HOME/Library/Android/sdk" ;;
        *)      SDK="/usr/local/lib/android/sdk" ;;
      esac
    fi
    NDK="$SDK/ndk/${ANDROID_NDK_VERSION:-28.2.13676358}"
  fi
  case "$(uname -s)" in
    Darwin) HOST_TAG=darwin-x86_64 ;;
    *)      HOST_TAG=linux-x86_64 ;;
  esac
  TOOLBIN="$NDK/toolchains/llvm/prebuilt/$HOST_TAG/bin"
  if [ ! -d "$TOOLBIN" ]; then
    echo "找不到 NDK 工具链：$TOOLBIN" >&2
    echo "  → 设 NDK=/path/to/ndk 或 ANDROID_HOME=/path/to/sdk" >&2
    exit 2
  fi
  CC="$TOOLBIN/${CLANG_TRIPLE}29-clang"
  # 交叉编译配置里的编译器路径随机器变，所以从模板生成，不写死。
  sed -e "s|@CC@|$CC|" \
      -e "s|@CXX@|$TOOLBIN/${CLANG_TRIPLE}29-clang++|" \
      -e "s|@AR@|$TOOLBIN/llvm-ar|" \
      -e "s|@STRIP@|$TOOLBIN/llvm-strip|" \
      "$MESON_INI.in" > "$MESON_INI"
fi

# 源码：freetype 与 harfbuzz 的 tarball 不随仓库走（几十 MB），
# 第一次跑时按需下载。
fetch() {
  local name="$1" url="$2" dir="$3"
  if [ -d "$SRC/$dir" ]; then return 0; fi
  mkdir -p "$SRC"
  echo "下载 $name..." >&2
  curl -fsSL "$url" -o "$SRC/$name.tar.gz"
  tar xf "$SRC/$name.tar.gz" -C "$SRC"
  [ -d "$SRC/$name" ] && [ "$name" != "$dir" ] && mv "$SRC/$name" "$SRC/$dir"
  rm -f "$SRC/$name.tar.gz"
}
FT_DIR=freetype-2.13.3
HB_DIR=harfbuzz-10.2.0
fetch "$FT_DIR" "https://download.savannah.gnu.org/releases/freetype/$FT_DIR.tar.gz" "$FT_DIR"
fetch "$HB_DIR" "https://github.com/harfbuzz/harfbuzz/releases/download/10.2.0/$HB_DIR.tar.xz" "$HB_DIR"

# FreeType：最小依赖静态库。多个 ABI 共用一个源码树时，先 distclean 清掉上一个
# ABI 的目标文件，避免不同架构混用。
if [ ! -f "$OUT/lib/libfreetype.a" ]; then
  (cd "$SRC/$FT_DIR" && \
   { [ -f Makefile ] && make distclean > /dev/null 2>&1 || true; } && \
   CC="$CC" ./configure --prefix="$ROOT/$OUT" --disable-shared \
     --without-brotli --without-bzip2 --without-png --without-zlib --without-harfbuzz \
     $FT_CONF > /dev/null 2>&1 && \
   make -j8 > /dev/null 2>&1 && make install > /dev/null 2>&1) && echo "freetype OK" >&2
fi

# HarfBuzz：禁 glib/icu/cairo，freetype 可选（不用，shaping 自足）。
# 本机 macOS 不需要交叉文件；Android 用生成的交叉文件。
cross_args=()
[ -n "$MESON_INI" ] && cross_args=(--cross-file="$ROOT/$MESON_INI")
if [ ! -f "$OUT/lib/libharfbuzz.a" ]; then
  rm -rf "$SRC/$HB_DIR/build-$ABI_TAG"
  (cd "$SRC/$HB_DIR" && \
   meson setup build-$ABI_TAG "${cross_args[@]}" --prefix="$ROOT/$OUT" \
     -Dglib=disabled -Dicu=disabled -Dcairo=disabled -Dfreetype=disabled \
     -Dtests=disabled -Ddocs=disabled -Dbenchmark=disabled -Dintrospection=disabled \
     --default-library=static --buildtype=release > /dev/null 2>&1 && \
   ninja -C build-$ABI_TAG install > /dev/null 2>&1) && echo "harfbuzz OK" >&2
fi
ls -la "$OUT"/lib/*.a 2>/dev/null >&2
