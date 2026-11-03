package mygo

import (
	"context"
	"net"
	"sync"

	"github.com/egoist/mygo/internal/mdns"
)

// MDNSService is a service found on, or advertised to, the local network.
type MDNSService struct {
	// Instance is the user-facing name, "Office Speaker".
	Instance string
	// Type is the service type, "_http._tcp".
	Type string
	// Host is the target host name; empty uses this device's.
	Host string
	Port int
	// Addrs are addresses of Host, as answers arrive.
	Addrs []net.IP
	// TXT are the service's key=value strings.
	TXT []string
}

// MDNSModule finds services on the local network and advertises one. Packets
// are paced and duplicates dropped (see internal/mdns).
//
// A phone's Wi-Fi filters multicast unless the app holds the system's
// multicast lock, which the Android host does not take yet: a browse there
// finds nothing until it does.
type MDNSModule struct{}

// MDNS is the local network's mDNS.
var MDNS = &MDNSModule{}

// Browse asks for serviceType, "_http._tcp" for example, and calls found for
// each service seen. found runs on the engine's goroutine and must return
// quickly; cancel stops the browse. Queries back off over about fifteen
// seconds rather than repeating: browse again when the answer is stale.
func (m *MDNSModule) Browse(serviceType string, found func(MDNSService)) (cancel func(), err error) {
	needsApp("MDNS.Browse")
	engine, err := mdns.SharedEngine()
	if err != nil {
		return nil, err
	}
	ctx, stopQueries := context.WithCancel(context.Background())
	inner, err := engine.Browse(ctx, serviceType, func(s mdns.Service) {
		found(MDNSService{
			Instance: s.Instance,
			Type:     s.Type,
			Host:     s.Host,
			Port:     s.Port,
			Addrs:    s.Addrs,
			TXT:      s.TXT,
		})
	})
	if err != nil {
		stopQueries()
		return nil, err
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			stopQueries()
			inner()
		})
	}, nil
}

// Advertise answers for svc until stop runs. The device's own name is used
// when Host is empty, and its addresses when Addrs is.
func (m *MDNSModule) Advertise(svc MDNSService) (stop func(), err error) {
	needsApp("MDNS.Advertise")
	engine, err := mdns.SharedEngine()
	if err != nil {
		return nil, err
	}
	inner, err := engine.Advertise(mdns.Service{
		Instance: svc.Instance,
		Type:     svc.Type,
		Host:     svc.Host,
		Port:     svc.Port,
		Addrs:    svc.Addrs,
		TXT:      svc.TXT,
	})
	if err != nil {
		return nil, err
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			inner()
		})
	}, nil
}
