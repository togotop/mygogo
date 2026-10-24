//go:build !js

package mygo

import (
	"encoding/json/v2"
	"testing"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/wait"
)

func TestNetwork(t *testing.T) {
	set := func(online, metered bool) {
		onMain(func() { fb.Online, fb.Metered = online, metered })
	}
	set(true, false)
	defer set(false, false)
	if s := Network.Status(); s != (NetworkStatus{Online: true}) {
		t.Errorf("Status = %+v, want online", s)
	}
	set(true, true)
	if s := Network.Status(); s != (NetworkStatus{Online: true, Metered: true}) {
		t.Errorf("Status = %+v, want online and metered", s)
	}

	var got []NetworkStatus
	mainOK := true
	off := Network.OnChanged(func(s NetworkStatus) {
		got = append(got, s)
		mainOK = mainOK && isMainThread()
	})
	defer off()
	wait.Until(t, "the backend asked to watch the network", func() bool { return onMainValue(func() bool { return fb.WatchingNetwork }) })

	// The status when watching started is the one changes are told from,
	// and a report that changes nothing calls no listener.
	onMain(func() {
		fb.EmitNetwork(platform.NetworkStatus{Online: true, Metered: true})
		fb.EmitNetwork(platform.NetworkStatus{Online: false})
		fb.EmitNetwork(platform.NetworkStatus{Online: false})
		fb.EmitNetwork(platform.NetworkStatus{Online: true})
	})
	want := []NetworkStatus{{}, {Online: true}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("changes = %+v, want %+v", got, want)
	}
	if !mainOK {
		t.Error("OnChanged did not run on the main thread")
	}

	off()
	onMain(func() { fb.EmitNetwork(platform.NetworkStatus{Metered: true}) })
	if len(got) != len(want) {
		t.Errorf("changes after off = %+v, want %+v", got, want)
	}
}

// TestWifiEventNetworks checks that a scan event becomes the public list, and
// that an event without one is an empty list rather than nil.
func TestWifiEventNetworks(t *testing.T) {
	var event wifiEvent
	if err := json.Unmarshal([]byte(`{"kind":"networks","networks":[{"ssid":"AndroidWifi","bssid":"02:15:b2:00:00:01","rssi":-45,"security":"open"}]}`), &event); err != nil {
		t.Fatalf("decode: %v", err)
	}
	got := event.networks()
	if len(got) != 1 {
		t.Fatalf("networks = %+v", got)
	}
	want := WifiNetwork{SSID: "AndroidWifi", BSSID: "02:15:b2:00:00:01", RSSI: -45, Security: "open"}
	if got[0] != want {
		t.Errorf("network = %+v, want %+v", got[0], want)
	}
	if empty := (wifiEvent{Kind: "networks"}).networks(); len(empty) != 0 {
		t.Errorf("empty event = %+v", empty)
	}
}

// TestWifiErrorCarriesThePlatformMessage checks the fallback for an event that
// says nothing about what went wrong.
func TestWifiErrorCarriesThePlatformMessage(t *testing.T) {
	if got := wifiError("", "the scan failed").Error(); got != "mygo: the scan failed" {
		t.Errorf("fallback = %q", got)
	}
	if got := wifiError("throttled", "the scan failed").Error(); got != "mygo: throttled" {
		t.Errorf("platform message = %q", got)
	}
}

// TestNetworkStatusJSON checks the status's JSON, which pages get through
// an event.
func TestNetworkStatusJSON(t *testing.T) {
	b, err := json.Marshal(NetworkStatus{Online: true, Metered: true})
	if err != nil || string(b) != `{"online":true,"metered":true}` {
		t.Errorf("JSON = %s, %v", b, err)
	}
}
