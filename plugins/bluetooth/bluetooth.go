// Package bluetooth scans for Bluetooth LE devices and talks GATT to one at a
// time: what an app acting as a remote control, or reading a sensor, needs.
//
//	stop, err := bluetooth.Scan(ctx, "", func(d bluetooth.Device) { ... })
//	defer stop()
//	conn, err := bluetooth.Connect(ctx, address)
//	value, err := conn.Read(ctx, "2a37")
//
// The radio is the system's own (LeScanner and BluetoothGatt on Android);
// elsewhere its functions report errors.ErrUnsupported. A scan needs the
// system's permission to see devices, which the first Scan asks for.
package bluetooth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"sync"

	"github.com/egoist/mygo"
)

const pluginID = "bluetooth"

// Device is a Bluetooth LE device seen during a scan. RSSI is the signal
// strength in dBm, which is 0 for a device the platform did not report one
// for.
type Device struct {
	Address string
	Name    string
	RSSI    int
}

// Service is one service a connected device exposes. Characteristics use the
// short form ("2a37") when they sit on the Bluetooth base UUID, which is what
// the platform reports and what the GATT methods accept.
type Service struct {
	UUID            string   `json:"uuid"`
	Characteristics []string `json:"characteristics"`
}

// ErrDisconnected is the peripheral going away, either because it dropped the
// link or because Close was called.
var ErrDisconnected = errors.New("bluetooth: the connection dropped")

// ErrTimeout is the peripheral not answering in time.
var ErrTimeout = errors.New("bluetooth: the device did not answer")

// ErrCanceled is the user denying the permission the radio needs.
var ErrCanceled = errors.New("bluetooth: canceled")

// Available reports whether the device has a Bluetooth LE radio that is on.
func Available(ctx context.Context) (bool, error) {
	var r struct {
		Available bool `json:"available"`
	}
	if err := mygo.Native(pluginID).Call(ctx, "available", nil, &r); err != nil {
		return false, err
	}
	return r.Available, nil
}

// Scanner is a scan in progress: devices arrive through the callback given to
// Scan until Stop, which stops the radio.
type Scanner struct {
	once sync.Once
	stop func()
}

// Stop ends the scan.
func (s *Scanner) Stop() {
	if s != nil {
		s.once.Do(func() {
			if s.stop != nil {
				s.stop()
			}
		})
	}
}

// Scan looks for devices, reporting each to fn as it is seen, and keeps
// looking until Stop. service narrows the scan to the devices advertising it,
// in the short form ("180d") or the full UUID, or "" for all of them. The
// callback runs on the goroutine that called Scan.
func Scan(ctx context.Context, service string, fn func(Device)) (*Scanner, error) {
	events := make(chan Device, 32)
	off := mygo.Native(pluginID).On("event", func(data []byte) {
		var e struct {
			Kind    string `json:"kind"`
			Address string `json:"address"`
			Name    string `json:"name"`
			RSSI    int    `json:"rssi"`
		}
		if json.Unmarshal(data, &e) != nil || e.Kind != "device" {
			return
		}
		select {
		case events <- Device{Address: e.Address, Name: e.Name, RSSI: e.RSSI}:
		default:
			// A scan of a crowded room can see more than it draws: a device
			// that does not fit is one the next callback reports again.
		}
	})
	args := map[string]any{}
	if service != "" {
		args["service"] = service
	}
	if err := mygo.Native(pluginID).Call(ctx, "scan", args, nil); err != nil {
		off()
		return nil, err
	}
	done := make(chan struct{})
	go func() {
		for {
			select {
			case d := <-events:
				fn(d)
			case <-done:
				return
			}
		}
	}()
	s := &Scanner{stop: func() {
		close(done)
		off()
		_ = mygo.Native(pluginID).Call(context.Background(), "stopScan", nil, nil)
	}}
	return s, nil
}

// Conn is a GATT connection to one device, which reads and writes its
// characteristics and hears the ones it subscribes to.
type Conn struct {
	id       int64
	services []Service

	mu     sync.Mutex
	notify map[string]func([]byte)
	closed bool
	done   chan struct{}
}

// Connect connects to the device at address, as a scan reported it, and
// discovers its services. It fails with ErrTimeout when the device does not
// answer, and ErrCanceled when the permission to connect is denied.
func Connect(ctx context.Context, address string) (*Conn, error) {
	var r struct {
		ID       int64     `json:"id"`
		Services []Service `json:"services"`
	}
	err := mygo.Native(pluginID).Call(ctx, "connect", map[string]any{"address": address}, &r)
	if err != nil {
		return nil, mapError(err)
	}
	c := &Conn{id: r.ID, services: r.Services, notify: map[string]func([]byte){}, done: make(chan struct{})}
	off := mygo.Native(pluginID).On("event", c.event)
	go func() {
		<-c.done
		off()
	}()
	return c, nil
}

// event routes what the native side reports about this connection.
func (c *Conn) event(data []byte) {
	var e struct {
		Kind  string `json:"kind"`
		ID    int64  `json:"id"`
		Char  string `json:"char"`
		Value string `json:"value"`
		State int    `json:"state"`
	}
	if json.Unmarshal(data, &e) != nil || e.ID != c.id {
		return
	}
	switch e.Kind {
	case "value":
		value, err := base64.StdEncoding.DecodeString(e.Value)
		if err != nil {
			return
		}
		c.mu.Lock()
		fn := c.notify[strings.ToUpper(e.Char)]
		c.mu.Unlock()
		if fn != nil {
			fn(value)
		}
	case "state":
		if e.State == 0 {
			c.close(nil)
		}
	}
}

// close ends the connection once: the link is gone, or Close was called.
func (c *Conn) close(err error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	c.notify = map[string]func([]byte){}
	c.mu.Unlock()
	close(c.done)
}

// Services are the services the device exposed when it connected.
func (c *Conn) Services() []Service { return c.services }

// Done is closed when the connection ends, by Close or by the device going
// away.
func (c *Conn) Done() <-chan struct{} { return c.done }

// Read reads the value of a characteristic.
func (c *Conn) Read(ctx context.Context, characteristic string) ([]byte, error) {
	var r struct {
		Value string `json:"value"`
	}
	err := mygo.Native(pluginID).Call(ctx, "read", c.args(characteristic), &r)
	if err != nil {
		return nil, mapError(err)
	}
	return base64.StdEncoding.DecodeString(r.Value)
}

// Write writes value to a characteristic. withResponse asks the device to
// acknowledge the write, which it takes longer to do than a write without.
func (c *Conn) Write(ctx context.Context, characteristic string, value []byte, withResponse bool) error {
	args := c.args(characteristic)
	args["value"] = base64.StdEncoding.EncodeToString(value)
	args["response"] = withResponse
	if err := mygo.Native(pluginID).Call(ctx, "write", args, nil); err != nil {
		return mapError(err)
	}
	return nil
}

// Notify subscribes to a characteristic's notifications, whose values arrive
// through fn as the device sends them. The returned function unsubscribes.
func (c *Conn) Notify(ctx context.Context, characteristic string, fn func([]byte)) (func() error, error) {
	key := strings.ToUpper(characteristic)
	c.mu.Lock()
	c.notify[key] = fn
	c.mu.Unlock()
	if err := mygo.Native(pluginID).Call(ctx, "subscribe", c.args(characteristic), nil); err != nil {
		c.mu.Lock()
		delete(c.notify, key)
		c.mu.Unlock()
		return nil, mapError(err)
	}
	var once sync.Once
	return func() error {
		var err error
		once.Do(func() {
			c.mu.Lock()
			delete(c.notify, key)
			c.mu.Unlock()
			err = mygo.Native(pluginID).Call(context.Background(), "unsubscribe", c.args(characteristic), nil)
		})
		return err
	}, nil
}

// Close drops the connection. A notification still arriving is dropped.
func (c *Conn) Close() error {
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return nil
	}
	err := mygo.Native(pluginID).Call(context.Background(), "disconnect", map[string]any{"id": c.id}, nil)
	c.close(nil)
	return err
}

func (c *Conn) args(characteristic string) map[string]any {
	return map[string]any{"id": c.id, "char": characteristic}
}

// mapError turns the codes of the native side into the errors this package
// names.
func mapError(err error) error {
	var n *mygo.NativeError
	if errors.As(err, &n) {
		switch n.Code {
		case "Disconnected":
			return ErrDisconnected
		case "Timeout":
			return ErrTimeout
		case "Canceled", "Permission":
			return ErrCanceled
		}
	}
	return err
}
