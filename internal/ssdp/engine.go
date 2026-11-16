// Package ssdp finds devices on the local network the way UPnP and many
// smart-home devices announce themselves: one multicast M-SEARCH, then unicast
// answers.
//
// The protocol is small enough to speak here rather than pull a library in:
// the search is a fixed request, and an answer is a handful of headers. What
// the package does add is the discipline a shared network needs — a search
// sends at most two packets, answers are deduplicated by USN, and no packet
// larger than a sane size is read.
package ssdp

import (
	"context"
	"errors"
	"net"
	"strings"
	"time"
)

const (
	// group is the SSDP multicast group and port every device listens on.
	group = "239.255.255.250:1900"
	// maxPacket is what an answer may be; anything larger is not a search
	// answer this package reads.
	maxPacket = 8192
	// maxDevices caps one search, so a network that floods answers cannot
	// grow the caller's list without bound.
	maxDevices = 256
	// searchGap is how long a search waits before asking a second time: a
	// lost packet is common on Wi-Fi, a third ask is not worth the traffic.
	searchGap = 1500 * time.Millisecond
	// maxSearches is how many times one search asks.
	maxSearches = 2
	// mx is the window the protocol gives devices to answer, in seconds. It
	// keeps a busy network from answering at once.
	mx = "2"
)

// Device is one answer to an M-SEARCH.
type Device struct {
	// USN is the unique service name, which identifies the device and the
	// service it is answering for.
	USN string
	// Type is the search target it answered: a device type or a service
	// type, "urn:schemas-upnp-org:device:MediaRenderer:1" for example.
	Type string
	// Location is the URL of the device's description document.
	Location string
	// Server is what the device calls itself, often including its version.
	Server string
	// Addr is where the answer came from.
	Addr net.IP
	// Seen is when it arrived.
	Seen time.Time
}

// Search sends an M-SEARCH for service, "ssdp:all" for everything, and calls
// found for each device that answers until ctx ends. found runs on the calling
// goroutine and may block.
//
// A search that finds nothing is not an error: plenty of networks filter
// multicast, and plenty of networks have nothing to find.
func Search(ctx context.Context, service string, found func(Device)) error {
	if found == nil {
		return errors.New("ssdp: Search needs a callback")
	}
	if service == "" {
		service = "ssdp:all"
	}
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		return err
	}
	defer conn.Close()
	target, err := net.ResolveUDPAddr("udp4", group)
	if err != nil {
		return err
	}
	request := []byte("M-SEARCH * HTTP/1.1\r\n" +
		"HOST: " + group + "\r\n" +
		"MAN: \"ssdp:discover\"\r\n" +
		"MX: " + mx + "\r\n" +
		"ST: " + service + "\r\n" +
		"\r\n")

	// Closing the socket is what wakes a blocked read when the caller gives
	// up; the read then fails and the loop notices ctx.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()

	seen := make(map[string]bool, maxDevices)
	room := maxDevices
	for round := 0; round < maxSearches && room > 0; round++ {
		if _, err := conn.WriteToUDP(request, target); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		until := time.Now().Add(searchGap)
		if deadline, ok := ctx.Deadline(); ok && deadline.Before(until) {
			until = deadline
		}
		delivered, err := readUntil(conn, until, seen, found, room)
		room -= delivered
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return nil
		}
	}
	// Whatever is left of the caller's window: devices that answer at the
	// end of their MX window still count.
	if deadline, ok := ctx.Deadline(); ok && room > 0 {
		if _, err := readUntil(conn, deadline, seen, found, room); err != nil {
			return err
		}
	}
	return nil
}

// readUntil reads answers until the deadline passes, calling found for each
// device not already in seen, and returns how many it delivered.
func readUntil(conn *net.UDPConn, until time.Time, seen map[string]bool, found func(Device), room int) (int, error) {
	buf := make([]byte, maxPacket)
	delivered := 0
	for delivered < room {
		if err := conn.SetReadDeadline(until); err != nil {
			return delivered, err
		}
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				return delivered, nil
			}
			// A closed socket is the caller giving up.
			if errors.Is(err, net.ErrClosed) {
				return delivered, nil
			}
			return delivered, err
		}
		device, ok := parseAnswer(buf[:n], from)
		if !ok || seen[device.USN] {
			continue
		}
		seen[device.USN] = true
		delivered++
		found(device)
	}
	return delivered, nil
}

// parseAnswer turns one datagram into a device: only a 200 OK with a USN is
// an answer, and anything else on the group is not ours to read.
func parseAnswer(data []byte, from *net.UDPAddr) (Device, bool) {
	text := string(data)
	if !strings.HasPrefix(text, "HTTP/1.1 200") {
		return Device{}, false
	}
	headers := map[string]string{}
	for _, line := range strings.Split(text, "\r\n")[1:] {
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		headers[strings.ToUpper(strings.TrimSpace(name))] = strings.TrimSpace(value)
	}
	usn := headers["USN"]
	if usn == "" {
		return Device{}, false
	}
	device := Device{
		USN:      usn,
		Type:     headers["ST"],
		Location: headers["LOCATION"],
		Server:   headers["SERVER"],
		Seen:     time.Now(),
	}
	if device.Type == "" {
		device.Type = headers["NT"]
	}
	if from != nil {
		device.Addr = from.IP
	}
	return device, true
}
