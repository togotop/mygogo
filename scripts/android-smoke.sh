#!/usr/bin/env bash
# android-smoke.sh builds an example for the device adb reaches, installs it,
# walks its System page and checks what each plugin answered: the radio, the
# codes, the voice, the volume, the home screen's tile and widget, and the
# network. It needs a device or an emulator, and the SDK's aapt2.
#
#   bash scripts/android-smoke.sh                       # the first device
#   ANDROID_SERIAL=emulator-5554 bash scripts/android-smoke.sh
#   MYGO_SMOKE_APP=examples/todo bash scripts/android-smoke.sh
#
# Screenshots, the last UI dump and logcat land in .mygo/android-smoke/. The
# exit code is the number of checks that failed.
set -uo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"

app=${MYGO_SMOKE_APP:-examples/mobile}
out=$root/.mygo/android-smoke
rm -rf "$out"
mkdir -p "$out"

failures=0
pass() { printf '  \033[32mok\033[0m   %s\n' "$*"; }
fail() { printf '  \033[31mFAIL\033[0m %s\n' "$*"; failures=$((failures + 1)); }
step() { printf '\n\033[1m%s\033[0m\n' "$*"; }

# --- the device ---
serial=${ANDROID_SERIAL:-}
if [[ -z $serial ]]; then
	serial=$(adb devices | awk 'NR > 1 && $2 == "device" { print $1; exit }')
fi
if [[ -z $serial ]]; then
	echo "android-smoke: no device: start an emulator, or set ANDROID_SERIAL" >&2
	exit 1
fi
adb_() { adb -s "$serial" "$@"; }

abi=$(adb_ shell getprop ro.product.cpu.abi | tr -d '\r')
case $abi in
	x86_64) arch=amd64 ;;
	arm64-v8a) arch=arm64 ;;
	armeabi-v7a) arch=arm ;;
	x86) arch=386 ;;
	*) echo "android-smoke: unknown ABI $abi" >&2; exit 1 ;;
esac
size=$(adb_ shell wm size | awk -F': ' 'NR == 1 { print $2 }' | tr -d '\r')
w=${size%x*}
h=${size#*x}
echo "device $serial ($abi, ${w}x${h})"

# --- build and install ---
step "Building $app for android/$arch"
if ! go run ./cmd/mygo build -platform "android/$arch" -o "$out/build" "$app" > "$out/build.log" 2>&1; then
	tail -25 "$out/build.log"
	exit 1
fi
apk=$(command find "$out/build" -name '*.apk' -print -quit)
if [[ -z $apk ]]; then
	echo "android-smoke: the build left no APK" >&2
	exit 1
fi

aapt2=$(ls "${ANDROID_HOME:-$HOME/Android/Sdk}"/build-tools/*/aapt2 2>/dev/null | tail -1)
if [[ -z $aapt2 ]]; then
	echo "android-smoke: no aapt2 in the SDK's build-tools" >&2
	exit 1
fi
pkg=$("$aapt2" dump badging "$apk" 2>/dev/null | awk -F"'" '/^package: name=/ { print $2; exit }')
if [[ -z $pkg ]]; then
	echo "android-smoke: cannot read the package of $apk" >&2
	exit 1
fi
echo "package $pkg"

step "Installing $apk"
if ! adb_ install -r "$apk" > "$out/install.log" 2>&1; then
	tail -10 "$out/install.log"
	exit 1
fi
activity=$(adb_ shell cmd package resolve-activity --brief "$pkg" | tr -d '\r' | tail -1)
if [[ $activity != */* ]]; then
	echo "android-smoke: cannot resolve the launch activity of $pkg" >&2
	exit 1
fi

# --- the app ---
step "Starting $activity"
adb_ logcat -c
adb_ shell am force-stop "$pkg"
adb_ shell am start -n "$activity" > "$out/start.log" 2>&1
sleep 6
if ! adb_ shell pidof "$pkg" > /dev/null; then
	echo "android-smoke: $pkg is not running after the start" >&2
	adb_ logcat -d > "$out/logcat.txt"
	exit 1
fi

# dump takes the screen's accessibility tree, as a file.
dump() {
	rm -f "$out/ui.xml"
	adb_ shell uiautomator dump /sdcard/ui.xml > /dev/null 2>&1
	adb_ shell cat /sdcard/ui.xml 2>/dev/null | tr -d '\r' > "$out/ui.xml"
}

# find_center prints the center of the node whose text is want, or starts with
# "want,", which is how a row with its subtitle reads.
find_center() {
	python3 - "$out/ui.xml" "$1" <<'PY'
import re, sys
import xml.etree.ElementTree as ET
path, want = sys.argv[1], sys.argv[2]
try:
    tree = ET.parse(path).getroot()
except Exception:
    sys.exit(0)
for n in tree.iter('node'):
    t = n.get('text', '')
    if t == want or t.startswith(want + ','):
        m = re.match(r'\[(\d+),(\d+)\]\[(\d+),(\d+)\]', n.get('bounds', ''))
        if m:
            x1, y1, x2, y2 = map(int, m.groups())
            if x2 > x1 and y2 > y1:
                print((x1 + x2) // 2, (y1 + y2) // 2)
                sys.exit(0)
PY
}

# tap_text taps the center of a node, scrolling to find it. A node under the
# bottom bar is scrolled up first: its center would land on a tab.
tap_text() {
	local want=$1 i xy x y
	for i in 1 2 3 4 5 6; do
		dump
		xy=$(find_center "$want")
		if [[ -n $xy ]]; then
			x=${xy% *}
			y=${xy#* }
			if ((y > h * 4 / 5)); then
				adb_ shell input swipe "$((w / 2))" "$((h * 3 / 4))" "$((w / 2))" "$((h / 2))" 300
				sleep 1
				continue
			fi
			if ((y < h / 6)); then
				adb_ shell input swipe "$((w / 2))" "$((h / 3))" "$((w / 2))" "$((h * 2 / 3))" 300
				sleep 1
				continue
			fi
			adb_ shell input tap "$x" "$y"
			sleep 1
			return 0
		fi
		adb_ shell input swipe "$((w / 2))" "$((h * 2 / 3))" "$((w / 2))" "$((h / 2))" 250
		sleep 1
	done
	return 1
}

# tap_tab taps the bottom bar's tabs, which live where a row's center must
# not: no scrolling, and no bottom guard.
tap_tab() {
	local want=$1 i xy
	for i in 1 2 3; do
		dump
		xy=$(find_center "$want")
		if [[ -n $xy ]]; then
			adb_ shell input tap $xy
			sleep 1
			return 0
		fi
		sleep 1
	done
	return 1
}

text_of() {
	python3 - "$out/ui.xml" "$1" <<'PY'
import sys
import xml.etree.ElementTree as ET
path, want = sys.argv[1], sys.argv[2]
try:
    tree = ET.parse(path).getroot()
except Exception:
    sys.exit(0)
for n in tree.iter('node'):
    t = n.get('text', '')
    if t == want or t.startswith(want + ','):
        print(t)
        sys.exit(0)
PY
}

# row taps a row of the System page and checks what it answered: a pattern is
# a whole subtitle, and one of them must match.
row() {
	local title=$1
	shift
	if ! tap_text "$title"; then
		fail "$title: not on the screen"
		return
	fi
	# A scan, a voice starting or a network read takes a moment: the subtitle
	# is polled for, until it matches one of the patterns.
	local got pattern deadline=$((SECONDS + 12))
	while ((SECONDS < deadline)); do
		dump
		got=$(text_of "$title")
		for pattern in "$@"; do
			if [[ $got =~ $pattern ]]; then
				pass "$got"
				return
			fi
		done
		sleep 1
	done
	if [[ -z $got ]]; then
		fail "$title: no subtitle after the tap"
	else
		fail "$title: answered \"$got\""
	fi
}

step "The System page"
if ! tap_tab "System"; then
	fail "the System tab is not on the screen"
else
	row "Lower the volume" '^Lower the volume, [0-9]{1,3}% \(ringer (normal|vibrate|silent)\)$'
	# Back where it was: a run leaves the device's volume as it found it.
	row "Raise the volume" '^Raise the volume, [0-9]{1,3}% \(ringer (normal|vibrate|silent)\)$'
	row "Ask for silent" '^Ask for silent, (the ringer is silent|the system wants the do not disturb access)$'
	row "How is the network" '^How is the network, (wifi|cellular|ethernet|vpn|other|none), online (true|false), metered (true|false), constrained (true|false)$'
	row "Nearby Bluetooth LE" '^Nearby Bluetooth LE, (nothing in reach|[0-9]+ seen: .+)$'
	row "Read an NFC tag" '^Read an NFC tag, .+$'
	row "Say it aloud" '^Say it aloud, (the voice is saying it|Unavailable: .*)$'
	row "Set the tile" '^Set the tile, pull the quick settings down$'
	row "Set the widget" '^Set the widget, add it from the home screen$'
fi

# --- the crash and the artifacts ---
step "Checking the log"
adb_ exec-out screencap -p > "$out/screen.png" 2>/dev/null
pid=$(adb_ shell pidof "$pkg" | tr -d '\r' | awk '{ print $1 }')
if [[ -n $pid ]]; then
	# Only the app's own lines: uiautomator dumps crash now and then on a
	# device, and that crash is not the app's.
	adb_ logcat -d --pid="$pid" > "$out/logcat.txt" 2>/dev/null
else
	: > "$out/logcat.txt"
fi
if grep -q 'FATAL EXCEPTION' "$out/logcat.txt"; then
	fail "a Java exception killed the app"
	grep -A 8 'FATAL EXCEPTION' "$out/logcat.txt" | head -20
else
	pass "no Java exception"
fi
if [[ -n $pid ]]; then
	pass "$pkg is still running"
else
	fail "$pkg died during the run"
fi

step "Done"
if ((failures > 0)); then
	echo "$failures check(s) failed; artifacts in ${out#"$root"/}" >&2
	exit "$failures"
fi
echo "every check passed; artifacts in ${out#"$root"/}"
