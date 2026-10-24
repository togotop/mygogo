#!/usr/bin/env bash
# 构建 MyGo 的 Android 原生依赖 HarfBuzz/FreeType（静态库），产出可直接被
# Android 构建消费的路径，省得再去翻旧目录找（尤其 x86_64 的库）。
#
# 用法:
#   scripts/build-android-natives.sh [ABI...]
#     默认 ABI: arm64-v8a x86_64
#
# 每个 ABI 从源码编译（third_party/hb-build/build.sh），产出
# third_party/hb-build/lib/android-<tag>/{include,lib}。
#
# 结束打印每个 ABI 的 hb 路径，可直接用于：
#   Retty: RETTY_HB_<ARCH>
#   MyGo : MYGO_HB_ANDROID
set -euo pipefail
here="$(cd "$(dirname "$0")/.." && pwd)"
cd "$here"

abis=("$@")
[ ${#abis[@]} -eq 0 ] && abis=(arm64-v8a x86_64)

tag_for() {
  case "$1" in
    arm64-v8a) echo arm64 ;;
    x86_64) echo x86_64 ;;
    *) echo "未知 ABI: $1" >&2; exit 2 ;;
  esac
}

upper_for() { echo "$1" | tr '[:lower:]' '[:upper:]'; }

# 编译 HarfBuzz/FreeType，stdout 只回传目录，进度都走 stderr。
build_hb() {
  local abi="$1" tag target dir
  tag="$(tag_for "$abi")"
  target=android
  [ "$tag" = x86_64 ] && target=android-x86_64
  echo "== HarfBuzz/FreeType ($abi)" >&2
  bash third_party/hb-build/build.sh "$target" 1>&2
  dir="third_party/hb-build/lib/android-$tag"
  if [ ! -f "$dir/lib/libharfbuzz.a" ] || [ ! -f "$dir/lib/libfreetype.a" ]; then
    echo "构建后缺少 $dir/lib/lib{harfbuzz,freetype}.a" >&2
    return 1
  fi
  printf '%s' "$dir"
}

fail=0
results=()
for abi in "${abis[@]}"; do
  if hb="$(build_hb "$abi")"; then :; else fail=1; hb=""; fi
  results+=("$abi|$hb")
done

echo
echo "== 汇总（供消费）"
for row in "${results[@]}"; do
  IFS='|' read -r abi hb <<< "$row"
  upper="$(upper_for "$(tag_for "$abi")")"
  echo "[$abi]"
  echo "  hb dir : ${hb:-<缺失>}"
  [ -n "$hb" ] && echo "  MyGo   : MYGO_HB_ANDROID=$hb"
  [ -n "$hb" ] && echo "  Retty  : RETTY_HB_$upper=$hb"
done

exit $fail
