//go:build android && cgo

package backend

import (
	"time"

	"github.com/egoist/mygo/internal/platform"
)

// Network reports the connection the phone has, which is the system's answer:
// a network the app can use, and whether it is metered.
func (b *Backend) Network() platform.Network { return androidNetwork{b: b} }

type androidNetwork struct{ b *Backend }

// Status asks the system, so it costs one JNI call.
func (n androidNetwork) Status() platform.NetworkStatus {
	bridge := platformBridge()
	if bridge == nil {
		return platform.NetworkStatus{}
	}
	online, metered := bridge.NetworkStatus()
	return platform.NetworkStatus{Online: online, Metered: metered}
}

// Multicast holds the Wi-Fi multicast lock, which is what makes mDNS and SSDP
// reach the app at all: the stack drops multicast addressed to an app without
// it. The Java side keeps one lock and releases it on off.
func (n androidNetwork) Multicast(on bool) error {
	bridge := platformBridge()
	if bridge == nil {
		return ErrPlatformBridge
	}
	return bridge.Multicast(on)
}

// Wifi asks the system for the current network. SSID and BSSID are empty when
// the app lacks the permission the system requires (location, or the nearby
// devices permission), which is the platform's answer and not an error.
func (n androidNetwork) Wifi() platform.WifiInfo {
	ssid, bssid, connected := "", "", false
	if bridge := platformBridge(); bridge != nil {
		ssid, bssid, connected = bridge.WifiInfo()
	}
	return platform.WifiInfo{SSID: ssid, BSSID: bssid, Connected: connected}
}

// androidNetwork is what the Go side asserts for Wi-Fi scanning and joining;
// the assertion keeps the compiler checking the method names.
var _ platform.WifiScanner = androidNetwork{}

// The Wi-Fi scan and join half. Both need the nearby devices permission on
// Android 13 and later and location before that; the Java side reports which.

// ScanWifi starts a scan; TakeWifiEvent pulls what it found.
func (n androidNetwork) ScanWifi() int {
	if bridge := platformBridge(); bridge != nil {
		return bridge.WifiScan()
	}
	return 3
}

// TakeWifiScanEvent pulls one queued event, as JSON.
func (n androidNetwork) TakeWifiScanEvent() (string, bool) {
	if bridge := platformBridge(); bridge != nil {
		return bridge.TakeWifiEvent()
	}
	return "", false
}

// JoinWifi joins the network in json, which routes this process through it.
func (n androidNetwork) JoinWifi(json string) int {
	if bridge := platformBridge(); bridge != nil {
		return bridge.WifiJoin(json)
	}
	return 3
}

// LeaveWifi drops it and restores the default route.
func (n androidNetwork) LeaveWifi() int {
	if bridge := platformBridge(); bridge != nil {
		return bridge.WifiLeave()
	}
	return 3
}

// Proxy asks the JVM's proxy selector, which Android feeds from the system
// settings (and from the Wi-Fi network's own proxy).
func (n androidNetwork) Proxy() platform.ProxyInfo {
	host, port, ok := "", 0, false
	if bridge := platformBridge(); bridge != nil {
		host, port, ok = bridge.SystemProxy()
	}
	if !ok {
		return platform.ProxyInfo{}
	}
	return platform.ProxyInfo{Host: host, Port: port}
}

// networkPoll is how often Watch asks, since ConnectivityManager reports
// changes through a callback this bridge does not carry.
const networkPoll = 5 * time.Second

// Watch reports changes to the app every networkPoll. The first answer is
// reported too, as the desktop backends' first report is.
func (n androidNetwork) Watch() {
	n.b.networkOnce.Do(func() {
		go func() {
			var last platform.NetworkStatus
			for {
				now := n.Status()
				if now != last {
					last = now
					n.b.Post(func() {
						if h := n.b.handler(); h != nil {
							h.NetworkChanged(now)
						}
					})
				}
				select {
				case <-n.b.quit:
					return
				case <-time.After(networkPoll):
				}
			}
		}()
	})
}
