package mygo

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// BLEDevice is a Bluetooth LE device seen during a scan. RSSI is the signal
// strength in dBm.
type BLEDevice struct {
	Address string
	Name    string
	RSSI    int
}

// BLEService is one service a connected device exposes. Characteristics use
// the short form ("2a37") when they sit on the Bluetooth base UUID, which is
// what the platform reports and what the GATT methods accept.
type BLEService struct {
	UUID            string
	Characteristics []string
}

// ErrBLEDisconnected is the peripheral going away, either because it dropped
// the link or because Close was called.
var ErrBLEDisconnected = errors.New("mygo: the Bluetooth LE connection dropped")

// ErrBLETimeout is the peripheral not answering in time.
var ErrBLETimeout = errors.New("mygo: the Bluetooth LE device did not answer")

// BluetoothModule scans for Bluetooth LE advertisements and talks GATT to one
// device at a time, which is what a phone acting as a remote control needs.
type BluetoothModule struct{}

// Bluetooth is the device's Bluetooth LE radio.
var Bluetooth = &BluetoothModule{}

const (
	// blePoll is how often the drain loop asks the platform for queued events:
	// the Java side collects them, and this side spreads them out.
	blePoll = 120 * time.Millisecond
	// bleReplyTimeout bounds one read or acknowledged write.
	bleReplyTimeout = 5 * time.Second
)

// bleEvent is one queued message from the platform.
type bleEvent struct {
	Kind           string `json:"kind"`
	Address        string `json:"address"`
	Name           string `json:"name"`
	RSSI           int    `json:"rssi"`
	Characteristic string `json:"characteristic"`
	Value          string `json:"value"`
	Status         int    `json:"status"`
	Notify         bool   `json:"notify"`
	Error          string `json:"error"`
	State          int    `json:"state"`
	MTU            int    `json:"mtu"`
	Services       []struct {
		UUID            string   `json:"uuid"`
		Characteristics []string `json:"characteristics"`
	} `json:"services"`
}

// bleHub owns the one event queue the platform fills. A single goroutine drains
// it while a scan or a connection needs it and routes what it finds: scan
// results go to the scan callback, everything else to the open connection.
// Draining stops as soon as neither is there, so an idle app is not polling.
type bleEventHub struct {
	mu      sync.Mutex
	running bool
	scan    func(BLEDevice)
	conn    *BLEConnection
}

var bleHub bleEventHub

// start makes sure the drain loop is running. Call with the hub unlocked.
func (h *bleEventHub) start() {
	h.mu.Lock()
	if h.running {
		h.mu.Unlock()
		return
	}
	h.running = true
	h.mu.Unlock()
	go h.drain()
}

func (h *bleEventHub) drain() {
	for {
		h.mu.Lock()
		if h.scan == nil && h.conn == nil {
			// Both checks and the reset happen under the lock, so a scan or a
			// connection that starts now sees running == false and restarts
			// the loop rather than queueing into a goroutine that is leaving.
			h.running = false
			h.mu.Unlock()
			return
		}
		h.mu.Unlock()

		var raw string
		var ok bool
		onMain(func() { raw, ok = backend().Mobile().TakeBLEEvent() })
		if ok {
			h.mu.Lock()
			scan, conn := h.scan, h.conn
			h.mu.Unlock()
			dispatchBLE(raw, scan, conn)
		}
		time.Sleep(blePoll)
	}
}

// dispatchBLE routes one queued event to whoever asked for it.
func dispatchBLE(raw string, scan func(BLEDevice), conn *BLEConnection) {
	var event bleEvent
	if err := json.Unmarshal([]byte(raw), &event); err != nil {
		return
	}
	if event.Kind == "advertisement" {
		if scan != nil && event.Address != "" {
			scan(BLEDevice{Address: event.Address, Name: event.Name, RSSI: event.RSSI})
		}
		return
	}
	if conn != nil {
		conn.handle(event)
	}
}

// Scan listens for advertisements until timeout and calls found for each new
// device. A name needs the connect permission on Android 12 and later; without
// it the address and signal strength still arrive. found runs on a background
// goroutine, so it may block without holding the radio up.
//
// ErrUnsupported means the device has no adapter, and ErrPermissionDenied
// means the app may not scan.
func (m *BluetoothModule) Scan(timeout time.Duration, found func(BLEDevice)) error {
	needsApp("Bluetooth.Scan")
	if found == nil {
		return errors.New("mygo: Bluetooth.Scan needs a callback")
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	bleHub.mu.Lock()
	if bleHub.scan != nil {
		bleHub.mu.Unlock()
		return errors.New("mygo: a Bluetooth scan is already running")
	}
	bleHub.mu.Unlock()
	var startErr error
	onMain(func() {
		var rc int
		rc, startErr = backend().Mobile().StartBLEScan("{}")
		if startErr == nil && rc != 0 {
			// The platform reported a code without an error of its own.
			startErr = errors.New("mygo: the scan did not start")
		}
	})
	if startErr != nil {
		return startErr
	}
	bleHub.mu.Lock()
	bleHub.scan = found
	bleHub.mu.Unlock()
	bleHub.start()
	defer func() {
		bleHub.mu.Lock()
		bleHub.scan = nil
		bleHub.mu.Unlock()
		_ = m.StopScan()
	}()
	pauseMain(timeout)
	return nil
}

// pauseMain waits for d, keeping the main thread's queue moving when it is the
// one waiting: the drain goroutine hands events over through that queue.
func pauseMain(d time.Duration) {
	if !isMainThread() {
		time.Sleep(d)
		return
	}
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		backend().Step()
	}
}

// waitPumped waits for one value on ch or for the timeout, pumping the main
// thread's queue while it does. Blocking on a channel from the main thread
// would stop the drain goroutine from delivering the very event being waited
// for.
func waitPumped[T any](ch <-chan T, timeout time.Duration) (T, bool) {
	var zero T
	if !isMainThread() {
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case v := <-ch:
			return v, true
		case <-timer.C:
			return zero, false
		}
	}
	deadline := time.Now().Add(timeout)
	for {
		select {
		case v := <-ch:
			return v, true
		default:
		}
		if !time.Now().Before(deadline) {
			return zero, false
		}
		backend().Step()
	}
}

// StopScan ends a scan early; Scan stops its own scan when it returns.
func (m *BluetoothModule) StopScan() error {
	needsApp("Bluetooth.StopScan")
	var err error
	onMain(func() { err = backend().Mobile().StopBLEScan() })
	return err
}

// bleValue is one characteristic read: the bytes and the GATT status that came
// with them.
type bleValue struct {
	value  []byte
	status int
}

// bleStatusDropped stands in for a GATT status when the link went away before
// the peripheral answered, so a waiting caller learns that instead of timing
// out.
const bleStatusDropped = -1

// The bond states Android reports, the same numbers as BluetoothDevice.
const (
	bleBondNone    = 10
	bleBondBonding = 11
	bleBondBonded  = 12
)

// bleMTU is one negotiation result: what the peripheral agreed to and the
// status it came with.
type bleMTU struct {
	size   int
	status int
}

// BLEConnection is one open GATT link. The GATT methods are serialised by the
// platform: a stack refuses a second operation while one is in flight, which
// shows up here as an error.
type BLEConnection struct {
	// Address is the peripheral's address as the scan reported it.
	Address string

	mu     sync.Mutex
	closed bool
	gone   chan struct{}
	ready  chan error
	settle sync.Once

	services  []BLEService
	reads     map[string]chan bleValue
	writes    map[string]chan int
	notify    map[string]func([]byte)
	bondState int
	bondWait  chan int
	mtu       int
	mtuWait   chan bleMTU
}

// Connect connects to address and discovers its services. timeout bounds the
// whole handshake; zero means ten seconds. The connection stays open until
// Close or until the peripheral drops it.
func (m *BluetoothModule) Connect(address string, timeout time.Duration) (*BLEConnection, error) {
	needsApp("Bluetooth.Connect")
	address = strings.TrimSpace(address)
	if address == "" {
		return nil, errors.New("mygo: Bluetooth.Connect needs an address")
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	conn := &BLEConnection{
		Address: address,
		gone:    make(chan struct{}),
		ready:   make(chan error, 1),
		reads:   make(map[string]chan bleValue),
		writes:  make(map[string]chan int),
		notify:  make(map[string]func([]byte)),
	}
	bleHub.mu.Lock()
	if bleHub.conn != nil {
		other := bleHub.conn.Address
		bleHub.mu.Unlock()
		return nil, errors.New("mygo: already connected to " + other)
	}
	bleHub.conn = conn
	bleHub.mu.Unlock()
	bleHub.start()

	payload, _ := json.Marshal(map[string]string{"address": address})
	var err error
	onMain(func() { err = backend().Mobile().BLEConnect(string(payload)) })
	if err != nil {
		conn.detach()
		return nil, err
	}
	err, ok := waitPumped(conn.ready, timeout)
	if !ok {
		conn.Close()
		return nil, ErrBLETimeout
	}
	if err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

// Services is what the peripheral exposed when the connection was made, as a
// copy: the platform may still be filling it in on the drain goroutine.
func (c *BLEConnection) Services() []BLEService {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]BLEService, len(c.services))
	for i, s := range c.services {
		out[i] = BLEService{
			UUID:            s.UUID,
			Characteristics: append([]string(nil), s.Characteristics...),
		}
	}
	return out
}

// Read reads one characteristic: "1234/5678" for service and characteristic,
// or just the characteristic when the service does not matter.
func (c *BLEConnection) Read(characteristic string) ([]byte, error) {
	key := bleCharacteristicKey(characteristic)
	reply := make(chan bleValue, 1)
	if err := c.watchRead(key, reply); err != nil {
		return nil, err
	}
	defer c.forgetRead(key)

	var err error
	onMain(func() {
		err = backend().Mobile().BLERead(jsonPayload(map[string]any{"characteristic": characteristic}))
	})
	if err != nil {
		return nil, err
	}
	v, ok := waitPumped(reply, bleReplyTimeout)
	if !ok {
		return nil, ErrBLETimeout
	}
	switch v.status {
	case 0:
		return v.value, nil
	case bleStatusDropped:
		return nil, ErrBLEDisconnected
	}
	return nil, fmt.Errorf("mygo: reading %s failed with status %d", characteristic, v.status)
}

// Write writes value to one characteristic. withResponse asks the stack to wait
// for the peripheral to acknowledge it; a write without one returns as soon as
// it is queued, since there is nothing to wait for.
func (c *BLEConnection) Write(characteristic string, value []byte, withResponse bool) error {
	key := bleCharacteristicKey(characteristic)
	payload := jsonPayload(map[string]any{
		"characteristic": characteristic,
		"value":          base64.StdEncoding.EncodeToString(value),
		"response":       withResponse,
	})
	if !withResponse {
		var err error
		onMain(func() { err = backend().Mobile().BLEWrite(payload) })
		return err
	}
	reply := make(chan int, 1)
	if err := c.watchWrite(key, reply); err != nil {
		return err
	}
	defer c.forgetWrite(key)

	var err error
	onMain(func() { err = backend().Mobile().BLEWrite(payload) })
	if err != nil {
		return err
	}
	status, ok := waitPumped(reply, bleReplyTimeout)
	if !ok {
		return ErrBLETimeout
	}
	switch status {
	case 0:
		return nil
	case bleStatusDropped:
		return ErrBLEDisconnected
	}
	return fmt.Errorf("mygo: writing %s failed with status %d", characteristic, status)
}

// Bond starts pairing and waits for the outcome. A device that is already
// paired answers at once. timeout bounds the wait; zero means thirty seconds,
// because the user may have to confirm on the peripheral.
func (c *BLEConnection) Bond(timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	wait := make(chan int, 1)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return ErrBLEDisconnected
	}
	c.bondWait = wait
	c.mu.Unlock()
	var err error
	onMain(func() { err = backend().Mobile().BLEBond(jsonPayload(map[string]any{"address": c.Address})) })
	if err != nil {
		c.dropBondWait(wait)
		return err
	}
	state, ok := waitPumped(wait, timeout)
	if !ok {
		c.dropBondWait(wait)
		return ErrBLETimeout
	}
	switch state {
	case bleBondBonded:
		return nil
	case bleStatusDropped:
		return ErrBLEDisconnected
	}
	return errors.New("mygo: the Bluetooth LE pairing did not complete")
}

// Bonded reports whether the device is paired, as of the last bond event.
func (c *BLEConnection) Bonded() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.bondState == bleBondBonded
}

// RequestMTU asks for a larger packet size (23 to 517 bytes) and returns what
// the peripheral agreed to. The connection starts at 23.
func (c *BLEConnection) RequestMTU(mtu int) (int, error) {
	if mtu < 23 || mtu > 517 {
		return 0, fmt.Errorf("mygo: an MTU of %d is outside 23..517", mtu)
	}
	wait := make(chan bleMTU, 1)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return 0, ErrBLEDisconnected
	}
	c.mtuWait = wait
	c.mu.Unlock()
	var err error
	onMain(func() { err = backend().Mobile().BLERequestMTU(jsonPayload(map[string]any{"mtu": mtu})) })
	if err != nil {
		c.dropMTUWait(wait)
		return 0, err
	}
	reply, ok := waitPumped(wait, bleReplyTimeout)
	if !ok {
		c.dropMTUWait(wait)
		return 0, ErrBLETimeout
	}
	switch {
	case reply.size == bleStatusDropped:
		return 0, ErrBLEDisconnected
	case reply.status != 0:
		return 0, fmt.Errorf("mygo: the MTU request failed with status %d", reply.status)
	}
	return reply.size, nil
}

// MTU is the last agreed packet size, or the 23 the connection starts with.
func (c *BLEConnection) MTU() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.mtu == 0 {
		return 23
	}
	return c.mtu
}

func (c *BLEConnection) dropBondWait(wait chan int) {
	c.mu.Lock()
	if c.bondWait == wait {
		c.bondWait = nil
	}
	c.mu.Unlock()
}

func (c *BLEConnection) dropMTUWait(wait chan bleMTU) {
	c.mu.Lock()
	if c.mtuWait == wait {
		c.mtuWait = nil
	}
	c.mu.Unlock()
}

// Subscribe asks the peripheral to push notifications from characteristic and
// calls handler with each value. handler runs on the drain goroutine, so it
// should hand work off rather than block, and it must not call back into the
// connection.
func (c *BLEConnection) Subscribe(characteristic string, handler func([]byte)) error {
	if handler == nil {
		return errors.New("mygo: Bluetooth.Subscribe needs a callback")
	}
	key := bleCharacteristicKey(characteristic)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return ErrBLEDisconnected
	}
	// Registered before the descriptor write: a peripheral may push a value
	// the moment notifications are on.
	c.notify[key] = handler
	c.mu.Unlock()
	var err error
	payload := jsonPayload(map[string]any{"characteristic": characteristic, "on": true})
	onMain(func() { err = backend().Mobile().BLESubscribe(payload) })
	if err != nil {
		c.mu.Lock()
		delete(c.notify, key)
		c.mu.Unlock()
	}
	return err
}

// Unsubscribe stops the notifications. The handler is dropped either way, even
// if the platform refuses the descriptor write.
func (c *BLEConnection) Unsubscribe(characteristic string) error {
	key := bleCharacteristicKey(characteristic)
	c.mu.Lock()
	delete(c.notify, key)
	c.mu.Unlock()
	var err error
	payload := jsonPayload(map[string]any{"characteristic": characteristic, "on": false})
	onMain(func() { err = backend().Mobile().BLESubscribe(payload) })
	return err
}

// Close drops the connection. Closing twice is not an error, and so is closing
// one the peripheral already dropped.
func (c *BLEConnection) Close() error {
	if c.markClosed() {
		return nil
	}
	var err error
	onMain(func() { err = backend().Mobile().BLEDisconnect("{}") })
	return err
}

// markClosed closes gone once, unhooks the connection from the hub, and reports
// whether it had already been closed.
func (c *BLEConnection) markClosed() bool {
	c.mu.Lock()
	already := c.closed
	c.closed = true
	if !already {
		close(c.gone)
	}
	c.mu.Unlock()
	if already {
		return true
	}
	c.releaseWaiters()
	c.detach()
	return false
}

// releaseWaiters answers everything waiting for a reply with the dropped
// status. Their channels are buffered, so this never blocks.
func (c *BLEConnection) releaseWaiters() {
	c.mu.Lock()
	reads, writes := c.reads, c.writes
	bond, mtu := c.bondWait, c.mtuWait
	c.reads = make(map[string]chan bleValue)
	c.writes = make(map[string]chan int)
	c.bondWait = nil
	c.mtuWait = nil
	c.mu.Unlock()
	for _, ch := range reads {
		select {
		case ch <- bleValue{status: bleStatusDropped}:
		default:
		}
	}
	for _, ch := range writes {
		select {
		case ch <- bleStatusDropped:
		default:
		}
	}
	if bond != nil {
		select {
		case bond <- bleStatusDropped:
		default:
		}
	}
	if mtu != nil {
		select {
		case mtu <- bleMTU{size: bleStatusDropped}:
		default:
		}
	}
}

// detach unhooks the connection from the hub so the drain loop can stop once
// nothing else needs it.
func (c *BLEConnection) detach() {
	bleHub.mu.Lock()
	if bleHub.conn == c {
		bleHub.conn = nil
	}
	bleHub.mu.Unlock()
}

// handle applies one event to the connection.
func (c *BLEConnection) handle(event bleEvent) {
	switch event.Kind {
	case "services":
		services := make([]BLEService, 0, len(event.Services))
		for _, s := range event.Services {
			services = append(services, BLEService{
				UUID:            s.UUID,
				Characteristics: append([]string(nil), s.Characteristics...),
			})
		}
		c.mu.Lock()
		c.services = services
		c.mu.Unlock()
		c.settleErr(nil)
	case "error":
		message := event.Error
		if message == "" {
			message = "the platform reported a Bluetooth error"
		}
		err := errors.New("mygo: " + message)
		c.settleErr(err)
		c.failWaits()
	case "value":
		value, decodeErr := base64.StdEncoding.DecodeString(event.Value)
		if decodeErr != nil {
			value = nil
		}
		key := bleCharacteristicKey(event.Characteristic)
		c.mu.Lock()
		reply := c.reads[key]
		delete(c.reads, key)
		handler := c.notify[key]
		c.mu.Unlock()
		if reply != nil {
			reply <- bleValue{value: value, status: event.Status}
			return
		}
		if handler != nil {
			handler(value)
		}
	case "written":
		key := bleCharacteristicKey(event.Characteristic)
		c.mu.Lock()
		reply := c.writes[key]
		delete(c.writes, key)
		c.mu.Unlock()
		if reply != nil {
			reply <- event.Status
		}
	case "bond":
		c.mu.Lock()
		c.bondState = event.State
		wait := c.bondWait
		if event.State != bleBondBonding {
			c.bondWait = nil
		}
		c.mu.Unlock()
		if wait != nil && event.State != bleBondBonding {
			select {
			case wait <- event.State:
			default:
			}
		}
	case "mtu":
		c.mu.Lock()
		c.mtu = event.MTU
		wait := c.mtuWait
		c.mtuWait = nil
		c.mu.Unlock()
		if wait != nil {
			select {
			case wait <- bleMTU{size: event.MTU, status: event.Status}:
			default:
			}
		}
	case "disconnected":
		c.markClosed()
		c.settleErr(ErrBLEDisconnected)
	}
}

// settleErr hands the connection's first outcome to Connect and ignores later
// ones: only the first describes whether the handshake worked.
func (c *BLEConnection) settleErr(err error) {
	c.settle.Do(func() {
		select {
		case c.ready <- err:
		default:
		}
	})
}

// failWaits releases everything waiting on a reply that will not come, and
// unhooks the connection so the drain loop can go quiet.
func (c *BLEConnection) failWaits() {
	c.markClosed()
}

func (c *BLEConnection) watchRead(key string, reply chan bleValue) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ErrBLEDisconnected
	}
	c.reads[key] = reply
	return nil
}

func (c *BLEConnection) forgetRead(key string) {
	c.mu.Lock()
	delete(c.reads, key)
	c.mu.Unlock()
}

func (c *BLEConnection) watchWrite(key string, reply chan int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ErrBLEDisconnected
	}
	c.writes[key] = reply
	return nil
}

func (c *BLEConnection) forgetWrite(key string) {
	c.mu.Lock()
	delete(c.writes, key)
	c.mu.Unlock()
}

// jsonPayload encodes one request for the platform without ever failing: every
// payload is a map of strings, numbers and booleans.
func jsonPayload(payload map[string]any) string {
	data, err := json.Marshal(payload)
	if err != nil {
		return "{}"
	}
	return string(data)
}

// bleCharacteristicKey reduces a characteristic reference to the short form the
// platform reports, so that "1234/5678", "5678" and the full base UUID all name
// the same notification.
const bleBaseUUID = "-0000-1000-8000-00805f9b34fb"

func bleCharacteristicKey(spec string) string {
	if i := strings.IndexByte(spec, '/'); i >= 0 {
		spec = spec[i+1:]
	}
	spec = strings.ToLower(strings.TrimSpace(spec))
	spec = strings.TrimPrefix(spec, "0x")
	if strings.HasPrefix(spec, "0000") && strings.HasSuffix(spec, bleBaseUUID) {
		if len(spec) == 4+len(bleBaseUUID)+4 {
			spec = spec[4 : 4+4]
		}
	}
	return spec
}
