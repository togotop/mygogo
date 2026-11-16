package ssdp

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

// TestParseAnswer checks the headers a device sends and the ones that make a
// datagram not an answer at all.
func TestParseAnswer(t *testing.T) {
	from := &net.UDPAddr{IP: net.ParseIP("192.168.1.20")}
	body := "HTTP/1.1 200 OK\r\n" +
		"CACHE-CONTROL: max-age=1800\r\n" +
		"LOCATION: http://192.168.1.20:49152/description.xml\r\n" +
		"SERVER: Linux/4.9 UPnP/1.0 Yeelight/1.0\r\n" +
		"ST: urn:schemas-upnp-org:device:basic:1\r\n" +
		"USN: uuid:2fa1dd74-1a2b-4f9e-9f4a-0123456789ab::urn:schemas-upnp-org:device:basic:1\r\n" +
		"\r\n"
	device, ok := parseAnswer([]byte(body), from)
	if !ok {
		t.Fatal("a 200 OK with a USN was not read as an answer")
	}
	if !strings.HasPrefix(device.USN, "uuid:2fa1dd74") {
		t.Errorf("USN = %q", device.USN)
	}
	if device.Type != "urn:schemas-upnp-org:device:basic:1" {
		t.Errorf("Type = %q", device.Type)
	}
	if device.Location != "http://192.168.1.20:49152/description.xml" {
		t.Errorf("Location = %q", device.Location)
	}
	if device.Server != "Linux/4.9 UPnP/1.0 Yeelight/1.0" {
		t.Errorf("Server = %q", device.Server)
	}
	if !device.Addr.Equal(from.IP) {
		t.Errorf("Addr = %v", device.Addr)
	}

	for _, tt := range []struct {
		name string
		body string
	}{
		{"not an answer", "NOTIFY * HTTP/1.1\r\nNT: upnp:rootdevice\r\nUSN: uuid:1\r\n\r\n"},
		{"no USN", "HTTP/1.1 200 OK\r\nST: ssdp:all\r\n\r\n"},
		{"empty", ""},
	} {
		if _, ok := parseAnswer([]byte(tt.body), from); ok {
			t.Errorf("%s: read as an answer", tt.name)
		}
	}
}

// TestParseAnswerHeaderCase checks that a device spelling its headers
// differently is still understood: the protocol does not fix their case.
func TestParseAnswerHeaderCase(t *testing.T) {
	body := "HTTP/1.1 200 OK\r\nlocation: http://10.0.0.5/d.xml\r\nusn: uuid:abc\r\nst: ssdp:all\r\n\r\n"
	device, ok := parseAnswer([]byte(body), nil)
	if !ok || device.USN != "uuid:abc" || device.Location != "http://10.0.0.5/d.xml" {
		t.Fatalf("device = %+v, ok = %v", device, ok)
	}
}

// TestSearchEndsWithTheContext checks that a search with nothing to find
// returns on time, with no error: an empty network is not a failure.
func TestSearchEndsWithTheContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	var found []Device
	if err := Search(ctx, "ssdp:all", func(d Device) { found = append(found, d) }); err != nil {
		t.Fatalf("search: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("search took %v, want about the context's 300ms", elapsed)
	}
	// A machine on a busy network may answer; what matters is that nothing
	// blocked or errored.
	_ = found
}

// TestSearchRejectsNoCallback checks the argument that would otherwise panic
// halfway through a read loop.
func TestSearchRejectsNoCallback(t *testing.T) {
	if err := Search(context.Background(), "ssdp:all", nil); err == nil {
		t.Fatal("Search without a callback returned nil")
	}
}
