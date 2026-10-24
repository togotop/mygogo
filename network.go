package mygo

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/internal/platform"
)

// NetworkModule reports whether the computer is online, and prepares the
// system network for Go's own sockets (Prepare). Use the Network singleton.
type NetworkModule struct {
	mu       sync.Mutex
	watching bool
	// last is the status listeners last saw, so they are only called when
	// something changed: the systems report changes of routes and
	// interfaces that leave the status as it was.
	last NetworkStatus

	onChanged listeners[func(NetworkStatus)]
	// prepared is set once Prepare succeeded.
	prepared bool
}

// Network reports whether the computer is online.
var Network = &NetworkModule{}

// WifiInfo names the Wi-Fi network the device is on. Empty fields mean the
// system hides them: no Wi-Fi, or a permission the app does not have.
type WifiInfo struct {
	SSID      string
	BSSID     string
	Connected bool
}

// WifiNetwork is one network a scan found. Security is one word: "open",
// "wep", "wpa", "wpa2", "wpa3" or "enterprise".
type WifiNetwork struct {
	SSID     string
	BSSID    string
	RSSI     int
	Security string
}

// WifiJoinOptions says which network to join. A device that broadcasts its own
// access point is provisioned this way: join it, talk to it, and leave once the
// credentials are handed over.
type WifiJoinOptions struct {
	SSID     string
	Password string
	// Security picks the passphrase type: "wpa3" joins with a SAE
	// passphrase, anything else with a WPA2 one. It is a hint; the access
	// point decides what it accepts.
	Security string
	// Timeout bounds the wait; zero means thirty seconds.
	Timeout time.Duration
}

// ErrWifiTimeout is a scan or join the system did not answer in time.
var ErrWifiTimeout = errors.New("mygo: the Wi-Fi operation timed out")

// wifiPoll is how often the event queue is asked for a new event while a scan
// or a join is running.
const wifiPoll = 150 * time.Millisecond

// wifiEvent is one queued message from the platform: a scan result, a join
// outcome, or an error.
type wifiEvent struct {
	Kind     string `json:"kind"`
	Error    string `json:"error"`
	SSID     string `json:"ssid"`
	Networks []struct {
		SSID     string `json:"ssid"`
		BSSID    string `json:"bssid"`
		RSSI     int    `json:"rssi"`
		Security string `json:"security"`
	} `json:"networks"`
}

// WifiNetworks scans for nearby networks and returns them when the system
// answers. timeout bounds the wait; zero means ten seconds.
//
// The "wifi" permission is what makes this work: Android 13 and later ask for
// the nearby devices permission, earlier versions for location. Without it the
// answer is platform.ErrPermissionDenied — or an empty list on a device whose
// system hides the results instead.
func (n *NetworkModule) WifiNetworks(timeout time.Duration) ([]WifiNetwork, error) {
	needsApp("Network.WifiNetworks")
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	var startErr error
	onMain(func() {
		scanner, ok := backend().Network().(platform.WifiScanner)
		if !ok {
			startErr = platform.ErrUnsupported
			return
		}
		switch rc := scanner.ScanWifi(); rc {
		case 0:
		case 1:
			startErr = platform.ErrPermissionDenied
		case 2:
			startErr = platform.ErrUnsupported
		default:
			startErr = errors.New("mygo: the Wi-Fi scan did not start")
		}
	})
	if startErr != nil {
		return nil, startErr
	}
	deadline := time.Now().Add(timeout)
	for {
		event, err := nextWifiEvent(time.Until(deadline))
		if err != nil {
			return nil, err
		}
		switch event.Kind {
		case "networks":
			return event.networks(), nil
		case "error":
			return nil, wifiError(event.Error, "the Wi-Fi scan failed")
		}
	}
}

// JoinWifi joins a network and routes this app's traffic through it. The
// "wifi" permission is required, and the whole process moves to that network
// until LeaveWifi: that is what makes talking to a device that has no internet
// of its own work.
func (n *NetworkModule) JoinWifi(o WifiJoinOptions) error {
	needsApp("Network.JoinWifi")
	o.SSID = strings.TrimSpace(o.SSID)
	if o.SSID == "" {
		return errors.New("mygo: Network.JoinWifi needs an SSID")
	}
	if o.Timeout <= 0 {
		o.Timeout = 30 * time.Second
	}
	payload, err := json.Marshal(map[string]string{
		"ssid":     o.SSID,
		"password": o.Password,
		"security": o.Security,
	})
	if err != nil {
		return err
	}
	var startErr error
	onMain(func() {
		scanner, ok := backend().Network().(platform.WifiScanner)
		if !ok {
			startErr = platform.ErrUnsupported
			return
		}
		switch rc := scanner.JoinWifi(string(payload)); rc {
		case 0:
		case 1:
			startErr = platform.ErrPermissionDenied
		case 2:
			startErr = platform.ErrUnsupported
		default:
			startErr = errors.New("mygo: the Wi-Fi join was refused")
		}
	})
	if startErr != nil {
		return startErr
	}
	deadline := time.Now().Add(o.Timeout)
	for {
		event, err := nextWifiEvent(time.Until(deadline))
		if err != nil {
			return err
		}
		switch event.Kind {
		case "joined":
			return nil
		case "join-failed":
			return wifiError(event.Error, "the access point did not answer")
		case "lost":
			return errors.New("mygo: the Wi-Fi connection dropped")
		}
	}
}

// LeaveWifi leaves the network JoinWifi joined and restores the default route.
// Leaving when there is nothing to leave is not an error.
func (n *NetworkModule) LeaveWifi() error {
	needsApp("Network.LeaveWifi")
	var err error
	onMain(func() {
		scanner, ok := backend().Network().(platform.WifiScanner)
		if !ok {
			err = platform.ErrUnsupported
			return
		}
		if scanner.LeaveWifi() != 0 {
			err = errors.New("mygo: the Wi-Fi network could not be left")
		}
	})
	return err
}

// networks turns the event's list into the public type.
func (e wifiEvent) networks() []WifiNetwork {
	found := make([]WifiNetwork, 0, len(e.Networks))
	for _, w := range e.Networks {
		found = append(found, WifiNetwork{SSID: w.SSID, BSSID: w.BSSID, RSSI: w.RSSI, Security: w.Security})
	}
	return found
}

// nextWifiEvent waits for one queued event, or fails when timeout runs out.
func nextWifiEvent(timeout time.Duration) (wifiEvent, error) {
	deadline := time.Now().Add(timeout)
	for {
		var raw string
		var ok bool
		onMain(func() {
			if scanner, isScanner := backend().Network().(platform.WifiScanner); isScanner {
				raw, ok = scanner.TakeWifiScanEvent()
			}
		})
		if ok {
			var event wifiEvent
			if json.Unmarshal([]byte(raw), &event) == nil {
				return event, nil
			}
		}
		if !time.Now().Before(deadline) {
			return wifiEvent{}, ErrWifiTimeout
		}
		pauseMain(wifiPoll)
	}
}

// wifiError turns the platform's message into an error, with a fallback for an
// event that carries none.
func wifiError(message, fallback string) error {
	if message == "" {
		message = fallback
	}
	return errors.New("mygo: " + message)
}

// ProxyInfo is the HTTP proxy the system has configured for the app. Empty
// means a direct connection.
type ProxyInfo struct {
	Host string
	Port int
}

// Proxy returns the system's HTTP proxy. Go's own sockets do not use it: an
// app that speaks HTTP passes Host and Port to its transport, and one that
// opens a tunnel has to speak CONNECT.
func (n *NetworkModule) Proxy() ProxyInfo {
	needsApp("Network.Proxy")
	var info ProxyInfo
	onMain(func() {
		if p, ok := backend().Network().(platform.ProxyProvider); ok {
			got := p.Proxy()
			info = ProxyInfo{Host: got.Host, Port: got.Port}
		}
	})
	return info
}

// Multicast keeps the Wi-Fi stack delivering multicast packets (mDNS, SSDP)
// while on. Android needs a lock for this: without it the stack drops packets
// addressed to this app, and no discovery library hears anything. Platforms
// that already deliver them do nothing and return nil.
func (n *NetworkModule) Multicast(on bool) error {
	needsApp("Network.Multicast")
	var err error
	onMain(func() {
		if w, ok := backend().Network().(platform.NetworkWifi); ok {
			err = w.Multicast(on)
		}
	})
	return err
}

// Wifi names the current Wi-Fi network. An empty answer is not an error: the
// platform may have no Wi-Fi at all, or the app may lack the permission the
// system asks for before naming a network.
func (n *NetworkModule) Wifi() WifiInfo {
	needsApp("Network.Wifi")
	var info WifiInfo
	onMain(func() {
		if w, ok := backend().Network().(platform.NetworkWifi); ok {
			got := w.Wifi()
			info = WifiInfo{SSID: got.SSID, BSSID: got.BSSID, Connected: got.Connected}
		}
	})
	return info
}

// NetworkStatus is the state of the computer's network connection. Pages
// get it as {"online": …, "metered": …}.
type NetworkStatus struct {
	// Online reports whether the system has a connection to a network
	// beyond the computer: a route out on macOS (Network.framework's
	// satisfied path) and Linux (GNetworkMonitor's network-available, a
	// default route), and on Windows a connectivity level of local
	// access or more. A network behind a captive portal, or one whose
	// way to the internet is down, counts, as it does for the browsers'
	// navigator.onLine: it does not prove that a server answers.
	Online bool `json:"online"`
	// Metered reports whether the connection costs by the amount of data
	// or is limited, so the app should send less and ask the user before
	// it downloads something large: an expensive path (cellular, a
	// phone's hotspot) or Low Data Mode on macOS, a metered connection on
	// Linux, and on Windows a cost that is fixed or variable, or one
	// that roams, is over its data limit or approaches it. It is false
	// in browsers, which do not say.
	Metered bool `json:"metered"`
}

// Status returns the computer's network state. It is safe from any
// goroutine; like the other calls that need the running app, it panics on
// the main goroutine before App.Run, and waits for Run elsewhere. On
// macOS the first call waits for the system's first answer, which comes
// within milliseconds.
func (n *NetworkModule) Status() NetworkStatus {
	needsApp("Network.Status")
	return onMainValue(func() NetworkStatus { return networkStatus(backend().Network().Status()) })
}

// OnChanged is called on the main thread when the computer's network state
// changes, with the new state.
func (n *NetworkModule) OnChanged(fn func(status NetworkStatus)) (off func()) {
	off = n.onChanged.add(fn, false)
	n.mu.Lock()
	start := !n.watching
	n.watching = true
	n.mu.Unlock()
	if start {
		App.WhenReady(func() {
			b := backend().Network()
			b.Watch()
			s := networkStatus(b.Status())
			n.mu.Lock()
			n.last = s
			n.mu.Unlock()
		})
	}
	return off
}

// changed records a status from the backend and calls the listeners when
// it differs from the last one.
func (n *NetworkModule) changed(status NetworkStatus) {
	n.mu.Lock()
	same := status == n.last
	n.last = status
	n.mu.Unlock()
	if !same {
		fire1(&n.onChanged, status)
	}
}

func networkStatus(s platform.NetworkStatus) NetworkStatus {
	return NetworkStatus{Online: s.Online, Metered: s.Metered}
}

var _ platform.Network // the backends implement it
