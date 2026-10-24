package mygo

import (
	"encoding/base64"
	"testing"
	"time"
)

// TestBLECharacteristicKey pins the reduction that lets a caller name a
// characteristic the long way and still match what the platform reports.
func TestBLECharacteristicKey(t *testing.T) {
	for _, tt := range []struct {
		spec string
		want string
	}{
		{"5678", "5678"},
		{"2A37", "2a37"},
		{"0x2a37", "2a37"},
		{"180f/2a19", "2a19"},
		{"00002a37-0000-1000-8000-00805f9b34fb", "2a37"},
		{"0000180f-0000-1000-8000-00805f9b34fb", "180f"},
		{"6e400003-b5a3-f393-e0a9-e50e24dcca9e", "6e400003-b5a3-f393-e0a9-e50e24dcca9e"},
		{" ", ""},
	} {
		if got := bleCharacteristicKey(tt.spec); got != tt.want {
			t.Errorf("bleCharacteristicKey(%q) = %q, want %q", tt.spec, got, tt.want)
		}
	}
}

// newTestConnection builds a connection without touching the platform, so the
// event routing can be exercised on its own.
func newTestConnection() *BLEConnection {
	return &BLEConnection{
		Address: "AA:BB:CC:DD:EE:FF",
		gone:    make(chan struct{}),
		ready:   make(chan error, 1),
		reads:   make(map[string]chan bleValue),
		writes:  make(map[string]chan int),
		notify:  make(map[string]func([]byte)),
	}
}

// TestBLEServicesEvent fills in the services and releases Connect.
func TestBLEServicesEvent(t *testing.T) {
	c := newTestConnection()
	c.handle(bleEvent{
		Kind:    "services",
		Address: c.Address,
		Services: []struct {
			UUID            string   `json:"uuid"`
			Characteristics []string `json:"characteristics"`
		}{
			{UUID: "180f", Characteristics: []string{"2a19"}},
		},
	})
	select {
	case err := <-c.ready:
		if err != nil {
			t.Fatalf("services settled with %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("services did not settle the handshake")
	}
	services := c.Services()
	if len(services) != 1 || services[0].UUID != "180f" {
		t.Fatalf("services = %+v", services)
	}
	if got := services[0].Characteristics; len(got) != 1 || got[0] != "2a19" {
		t.Fatalf("characteristics = %v", got)
	}
	// A later disconnection must not overwrite the handshake's outcome.
	c.handle(bleEvent{Kind: "disconnected", Address: c.Address})
	select {
	case err := <-c.ready:
		t.Fatalf("a second outcome was queued: %v", err)
	default:
	}
}

// TestBLEValueEvent answers a pending read and a notification handler, and
// leaves an unclaimed value alone rather than blocking.
func TestBLEValueEvent(t *testing.T) {
	c := newTestConnection()
	read := make(chan bleValue, 1)
	if err := c.watchRead("2a19", read); err != nil {
		t.Fatalf("watch: %v", err)
	}
	c.handle(bleEvent{Kind: "value", Characteristic: "2a19", Value: base64.StdEncoding.EncodeToString([]byte{0x64})})
	select {
	case v := <-read:
		if len(v.value) != 1 || v.value[0] != 0x64 {
			t.Fatalf("value = %v", v.value)
		}
	case <-time.After(time.Second):
		t.Fatal("read was not answered")
	}
	if _, ok := c.reads["2a19"]; ok {
		t.Fatal("the read waiter was not released")
	}

	pushed := make(chan []byte, 1)
	c.notify["2a19"] = func(value []byte) { pushed <- value }
	c.handle(bleEvent{Kind: "value", Characteristic: "2A19", Value: base64.StdEncoding.EncodeToString([]byte{7}), Notify: true})
	select {
	case v := <-pushed:
		if len(v) != 1 || v[0] != 7 {
			t.Fatalf("notification = %v", v)
		}
	case <-time.After(time.Second):
		t.Fatal("notification was not delivered")
	}

	// Nobody is waiting for this one; it must not panic or block.
	c.handle(bleEvent{Kind: "value", Characteristic: "2a00", Value: "AA=="})
}

// TestBLEWrittenEvent releases a pending acknowledged write.
func TestBLEWrittenEvent(t *testing.T) {
	c := newTestConnection()
	reply := make(chan int, 1)
	if err := c.watchWrite("2a19", reply); err != nil {
		t.Fatalf("watch: %v", err)
	}
	c.handle(bleEvent{Kind: "written", Characteristic: "2a19"})
	select {
	case status := <-reply:
		if status != 0 {
			t.Fatalf("status = %d", status)
		}
	case <-time.After(time.Second):
		t.Fatal("write was not acknowledged")
	}
}

// TestBLEBondEvent records the pairing state and answers the wait once, on the
// final state rather than on "bonding".
func TestBLEBondEvent(t *testing.T) {
	c := newTestConnection()
	wait := make(chan int, 1)
	c.mu.Lock()
	c.bondWait = wait
	c.mu.Unlock()
	c.handle(bleEvent{Kind: "bond", Address: c.Address, State: bleBondBonding})
	select {
	case state := <-wait:
		t.Fatalf("bonding settled the wait with %d", state)
	default:
	}
	if c.Bonded() {
		t.Fatal("bonded while still bonding")
	}
	c.handle(bleEvent{Kind: "bond", Address: c.Address, State: bleBondBonded})
	select {
	case state := <-wait:
		if state != bleBondBonded {
			t.Fatalf("bond state = %d", state)
		}
	case <-time.After(time.Second):
		t.Fatal("the bond wait was not answered")
	}
	if !c.Bonded() {
		t.Fatal("bond state was not recorded")
	}
}

// TestBLEMTUEvent records the negotiated size and answers the wait.
func TestBLEMTUEvent(t *testing.T) {
	c := newTestConnection()
	if c.MTU() != 23 {
		t.Fatalf("MTU before negotiation = %d", c.MTU())
	}
	wait := make(chan bleMTU, 1)
	c.mu.Lock()
	c.mtuWait = wait
	c.mu.Unlock()
	c.handle(bleEvent{Kind: "mtu", Address: c.Address, MTU: 247, Status: 0})
	select {
	case reply := <-wait:
		if reply.size != 247 || reply.status != 0 {
			t.Fatalf("mtu reply = %+v", reply)
		}
	case <-time.After(time.Second):
		t.Fatal("the MTU wait was not answered")
	}
	if c.MTU() != 247 {
		t.Fatalf("MTU after negotiation = %d", c.MTU())
	}
}

// TestBLEDisconnectEvent drops the connection and wakes everything waiting.
func TestBLEDisconnectEvent(t *testing.T) {
	c := newTestConnection()
	pending := make(chan bleValue, 1)
	if err := c.watchRead("2a19", pending); err != nil {
		t.Fatalf("watch: %v", err)
	}
	c.handle(bleEvent{Kind: "disconnected", Address: c.Address})
	select {
	case v := <-pending:
		if v.status != bleStatusDropped {
			t.Fatalf("pending read got status %d", v.status)
		}
	case <-time.After(time.Second):
		t.Fatal("pending read was not released")
	}
	select {
	case <-c.gone:
	default:
		t.Fatal("gone is still open")
	}
	if err := c.watchRead("2a19", make(chan bleValue, 1)); err != ErrBLEDisconnected {
		t.Fatalf("watchRead after disconnect = %v", err)
	}
	// Closing an already dropped connection reports that it was already
	// closed, and must not close gone a second time.
	if !c.markClosed() {
		t.Fatal("markClosed on a dropped connection was not a repeat")
	}
}
