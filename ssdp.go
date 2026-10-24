package mygo

import (
	"context"
	"errors"
	"net"
	"time"

	"github.com/egoist/mygo/internal/ssdp"
)

// SSDPDevice is a device that answered a search: a TV, a router, a light that
// speaks UPnP.
type SSDPDevice struct {
	// USN identifies the device and what it answered for, and is what tells
	// two answers apart.
	USN string
	// Type is the search target it answered, such as
	// "urn:schemas-upnp-org:device:MediaRenderer:1".
	Type string
	// Location is the URL of the device's description document, which is
	// where its services and control URLs are written down.
	Location string
	// Server is what the device calls itself.
	Server string
	// Addr is where the answer came from.
	Addr net.IP
	// Seen is when it arrived.
	Seen time.Time
}

// SSDPModule finds devices on the local network by search, which is how UPnP
// gear and a good deal of smart-home hardware announce themselves. On Android
// the Wi-Fi multicast lock is held for the length of a search.
type SSDPModule struct{}

// SSDP is the local network's SSDP.
var SSDP = &SSDPModule{}

// Search asks for service and calls found for each device that answers, until
// timeout. "ssdp:all" asks everything; a device type narrows it, and the device
// answers for each type it has.
//
// A search that finds nothing is not an error: networks filter multicast, and
// many have nothing to announce. found runs on Search's goroutine, so it may
// block, and Search returns once timeout has passed.
func (m *SSDPModule) Search(service string, timeout time.Duration, found func(SSDPDevice)) error {
	needsApp("SSDP.Search")
	if found == nil {
		return errors.New("mygo: SSDP.Search needs a callback")
	}
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	acquireMulticast()
	defer releaseMulticast()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return ssdp.Search(ctx, service, func(d ssdp.Device) {
		found(SSDPDevice{
			USN:      d.USN,
			Type:     d.Type,
			Location: d.Location,
			Server:   d.Server,
			Addr:     d.Addr,
			Seen:     d.Seen,
		})
	})
}
