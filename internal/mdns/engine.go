// Package mdns finds services on the local network and answers for one, with
// the guards a shared network needs: outgoing packets are paced, the same
// packet seen twice is dropped, and a browse sends a handful of queries that
// back off instead of repeating for ever.
//
// It speaks multicast DNS (RFC 6762) through miekg/dns and keeps the rest
// small: PTR, SRV, TXT and A records, one socket pair per process, IPv4 only.
package mdns

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
)

const (
	// group4 is the mDNS group and port.
	group4 = "224.0.0.251:5353"
	// maxPacket is what a message may be; a larger datagram is not one this
	// package reads, which is one way a flood cannot grow the process.
	maxPacket = 9000
	// maxServices caps what one browser tracks.
	maxServices = 256
	// sendRate and sendBurst pace outgoing packets: mDNS on a crowded network
	// fails when everyone talks at once, and a buggy caller must not become a
	// flood.
	sendRate  = 10.0
	sendBurst = 5.0
	// duplicateWindow drops a packet seen twice, which is what a multicast
	// loop or a chatty peer produces.
	duplicateWindow = time.Second
	// responseJitter spreads answers from several devices.
	responseJitterMin = 20 * time.Millisecond
	responseJitterMax = 120 * time.Millisecond
	// ttl is the lifetime this package gives its records.
	ttl = 120
	// cacheFlush marks a record unique to one host: a peer that receives it
	// drops what it had for that name and type. PTR is shared by every host
	// offering the service, so it never carries this bit.
	cacheFlush = 0x8000
)

// Service is a service found on the network, or one to advertise.
type Service struct {
	// Instance is the user-facing name, "Office Speaker".
	Instance string
	// Type is the service type, "_http._tcp".
	Type string
	// Host is the target host name, "speaker.local.".
	Host string
	// Port is the service's port.
	Port int
	// Addrs are the addresses of Host, as answers arrive.
	Addrs []net.IP
	// TXT are the service's key=value strings.
	TXT []string
}

// guard paces packets and drops duplicates.
type guard struct {
	mu     sync.Mutex
	tokens float64
	last   time.Time
	seen   map[[32]byte]time.Time
}

func newGuard() *guard {
	return &guard{tokens: sendBurst, last: time.Now(), seen: make(map[[32]byte]time.Time)}
}

// allow takes a token for one outgoing packet.
func (g *guard) allow() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	g.tokens += now.Sub(g.last).Seconds() * sendRate
	if g.tokens > sendBurst {
		g.tokens = sendBurst
	}
	g.last = now
	if g.tokens < 1 {
		return false
	}
	g.tokens--
	return true
}

// duplicate reports whether these bytes were seen within duplicateWindow.
func (g *guard) duplicate(b []byte) bool {
	sum := sha256.Sum256(b)
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	for k, at := range g.seen {
		if now.Sub(at) > duplicateWindow {
			delete(g.seen, k)
		}
	}
	if at, ok := g.seen[sum]; ok && now.Sub(at) <= duplicateWindow {
		g.seen[sum] = now
		return true
	}
	if len(g.seen) > 1024 {
		// A busy network must not grow this map for ever.
		g.seen = make(map[[32]byte]time.Time)
	}
	g.seen[sum] = now
	return false
}

// Engine is the process's mDNS socket pair: one joined to the group to read,
// one plain socket to send, so neither direction depends on the other's
// address binding.
type Engine struct {
	recv  *net.UDPConn
	send  *net.UDPConn
	group *net.UDPAddr
	guard *guard

	mu       sync.Mutex
	browsers []*browser
	adverts  []*Service
	// renames counts how often each original instance name was taken, so a
	// conflict becomes "Office (2)" rather than a new name every packet.
	renames map[string]int
	closed  bool
	done    chan struct{}
}

var (
	sharedOnce sync.Once
	shared     *Engine
	sharedErr  error
)

// SharedEngine returns the process's engine, opening the sockets on first use.
func SharedEngine() (*Engine, error) {
	sharedOnce.Do(func() {
		shared, sharedErr = open()
	})
	return shared, sharedErr
}

func open() (*Engine, error) {
	group, err := net.ResolveUDPAddr("udp4", group4)
	if err != nil {
		return nil, err
	}
	recv, err := net.ListenMulticastUDP("udp4", nil, group)
	if err != nil {
		return nil, err
	}
	_ = recv.SetReadBuffer(1 << 20)
	send, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		_ = recv.Close()
		return nil, err
	}
	e := &Engine{recv: recv, send: send, group: group, guard: newGuard(), renames: make(map[string]int), done: make(chan struct{})}
	go e.readLoop()
	return e, nil
}

func (e *Engine) readLoop() {
	buf := make([]byte, maxPacket)
	for {
		n, _, err := e.recv.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-e.done:
				return
			default:
			}
			continue
		}
		if n <= 12 || n > maxPacket {
			continue
		}
		packet := append([]byte(nil), buf[:n]...)
		if e.guard.duplicate(packet) {
			continue
		}
		var msg dns.Msg
		if err := msg.Unpack(packet); err != nil {
			continue
		}
		if msg.Response {
			// A response may claim a name this process advertises: another
			// host with the same default instance. Rename before the peers
			// see two services with one name.
			e.checkConflict(&msg)
			e.deliver(&msg)
			continue
		}
		e.answer(&msg)
	}
}

// deliver hands a response to the browsers that asked for its type.
func (e *Engine) deliver(msg *dns.Msg) {
	e.mu.Lock()
	browsers := append([]*browser(nil), e.browsers...)
	e.mu.Unlock()
	for _, b := range browsers {
		b.absorb(msg)
	}
}

// answer replies for an advertised service a query asks about.
func (e *Engine) answer(msg *dns.Msg) {
	e.mu.Lock()
	adverts := append([]*Service(nil), e.adverts...)
	e.mu.Unlock()
	for _, svc := range adverts {
		for _, q := range msg.Question {
			if q.Qtype != dns.TypePTR || q.Qclass != dns.ClassINET {
				continue
			}
			if !strings.EqualFold(q.Name, dns.Fqdn(svc.Type)) {
				continue
			}
			// A query that already carries our PTR with at least half its TTL
			// left needs no answer: that is known-answer suppression, and it
			// is what keeps a network of browsers quiet.
			if knownAnswer(msg, serviceName(svc.Instance, svc.Type)) {
				continue
			}
			// Shared records answer after a short random delay, so several
			// devices do not talk over each other.
			delay := responseJitterMin + time.Duration(rand.Int63n(int64(responseJitterMax-responseJitterMin)))
			reply := *svc
			query := msg
			time.AfterFunc(delay, func() { e.sendResponse(query, reply) })
			return
		}
	}
}

// knownAnswer reports whether the query already carries this instance's PTR
// with at least half its TTL left.
func knownAnswer(query *dns.Msg, instance string) bool {
	for _, rr := range query.Answer {
		p, ok := rr.(*dns.PTR)
		if !ok {
			continue
		}
		if strings.EqualFold(p.Ptr, instance) && p.Hdr.Ttl >= ttl/2 {
			return true
		}
	}
	return false
}

// checkConflict renames an advertised service when a response claims its
// instance name with another host or port. This is the first-come side of
// RFC 6762's conflict resolution, without the probing phase: a peer that was
// there first keeps the name.
func (e *Engine) checkConflict(msg *dns.Msg) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, svc := range e.adverts {
		name := serviceName(svc.Instance, svc.Type)
		for _, rr := range append(append([]dns.RR(nil), msg.Answer...), msg.Extra...) {
			srv, ok := rr.(*dns.SRV)
			if !ok || !strings.EqualFold(srv.Hdr.Name, name) {
				continue
			}
			if int(srv.Port) == svc.Port && strings.EqualFold(srv.Target, dns.Fqdn(svc.Host)) {
				continue // that is us
			}
			original := trimInstance(name)
			e.renames[original]++
			if e.renames[original] > 10 {
				e.renames[original] = 10
			}
			old := svc.Instance
			svc.Instance = renamedInstance(original, e.renames[original]+1)
			log.Printf("mdns: %q is taken by another host, advertising as %q", old, svc.Instance)
			renamed := *svc
			e.announce(renamed)
			break
		}
	}
}

// renamedInstance is "Office (2)" for the second claimant of "Office".
func renamedInstance(instance string, n int) string {
	return fmt.Sprintf("%s (%d)", instance, n)
}

// sendResponse answers query for svc.
func (e *Engine) sendResponse(query *dns.Msg, svc Service) {
	if !e.guard.allow() {
		return
	}
	resp := new(dns.Msg)
	resp.SetReply(query)
	resp.Authoritative = true
	resp.Answer = records(svc)
	e.write(resp)
}

// announce sends an unsolicited response twice: the second covers a first
// packet lost on a crowded network.
func (e *Engine) announce(svc Service) {
	for _, after := range []time.Duration{0, time.Second} {
		reply := svc
		time.AfterFunc(after, func() {
			if !e.guard.allow() {
				return
			}
			resp := new(dns.Msg)
			resp.Response = true
			resp.Authoritative = true
			resp.Answer = records(reply)
			e.write(resp)
		})
	}
}

func (e *Engine) write(msg *dns.Msg) {
	if e.send == nil {
		return
	}
	payload, err := msg.Pack()
	if err != nil || len(payload) > maxPacket {
		return
	}
	_, _ = e.send.WriteToUDP(payload, e.group)
}

// records is the answer set for a service: PTR, SRV, TXT and A.
func records(svc Service) []dns.RR {
	name := dns.Fqdn(svc.Type)
	instance := serviceName(svc.Instance, svc.Type)
	host := svc.Host
	if host == "" {
		host = hostName()
	}
	hdr := func(name string, t uint16) dns.RR_Header {
		return dns.RR_Header{Name: name, Rrtype: t, Class: dns.ClassINET | cacheFlush, Ttl: ttl}
	}
	out := []dns.RR{
		// Shared: every device offering this type answers with its own PTR.
		&dns.PTR{Hdr: dns.RR_Header{Name: name, Rrtype: dns.TypePTR, Class: dns.ClassINET, Ttl: ttl}, Ptr: instance},
		// Unique to this host: a peer replaces what it had for these names.
		&dns.SRV{Hdr: hdr(instance, dns.TypeSRV), Port: uint16(svc.Port), Target: dns.Fqdn(host)},
		&dns.TXT{Hdr: hdr(instance, dns.TypeTXT), Txt: svc.TXT},
	}
	for _, ip := range svc.Addrs {
		if v4 := ip.To4(); v4 != nil {
			out = append(out, &dns.A{Hdr: hdr(dns.Fqdn(host), dns.TypeA), A: v4})
			continue
		}
		if v6 := ip.To16(); v6 != nil {
			out = append(out, &dns.AAAA{Hdr: hdr(dns.Fqdn(host), dns.TypeAAAA), AAAA: v6})
		}
	}
	return out
}

// Browse asks for serviceType and calls found for each service seen. found
// runs on the engine's goroutine and must return quickly.
func (e *Engine) Browse(ctx context.Context, serviceType string, found func(Service)) (cancel func(), err error) {
	b := &browser{engine: e, kind: normalize(serviceType), found: found, last: make(map[string]string), byName: make(map[string]*Service), expiry: make(map[string]time.Time)}
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil, errors.New("mdns: the engine is closed")
	}
	e.browsers = append(e.browsers, b)
	e.mu.Unlock()

	stop := make(chan struct{})
	go b.queryLoop(ctx, stop)
	cancel = func() {
		select {
		case <-stop:
		default:
			close(stop)
		}
		e.mu.Lock()
		for i, x := range e.browsers {
			if x == b {
				e.browsers = append(e.browsers[:i], e.browsers[i+1:]...)
				break
			}
		}
		e.mu.Unlock()
	}
	return cancel, nil
}

// Advertise answers for svc until stop runs.
func (e *Engine) Advertise(svc Service) (stop func(), err error) {
	if svc.Instance == "" || svc.Type == "" || svc.Port <= 0 {
		return nil, errors.New("mdns: advertise needs an instance, a type and a port")
	}
	if svc.Host == "" {
		svc.Host = hostName()
	}
	// The records and the questions that reach them must name the same type:
	// "_http._tcp" becomes "_http._tcp.local." here, once, as browsing does.
	svc.Type = normalize(svc.Type)
	if len(svc.Addrs) == 0 {
		svc.Addrs = localAddrs()
	}
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil, errors.New("mdns: the engine is closed")
	}
	e.adverts = append(e.adverts, &svc)
	e.mu.Unlock()
	log.Printf("mdns: advertising %q %s at %v", svc.Instance, svc.Type, svc.Addrs)
	e.announce(svc)
	stop = func() {
		e.mu.Lock()
		for i, x := range e.adverts {
			if x == &svc {
				e.adverts = append(e.adverts[:i], e.adverts[i+1:]...)
				break
			}
		}
		e.mu.Unlock()
	}
	return stop, nil
}

// Close stops the engine; for tests and the end of the process.
func (e *Engine) Close() {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return
	}
	e.closed = true
	e.mu.Unlock()
	close(e.done)
	_ = e.recv.Close()
	_ = e.send.Close()
}

// normalize turns "_http._tcp" into "_http._tcp.local.".
func normalize(serviceType string) string {
	t := strings.TrimSuffix(serviceType, ".")
	if !strings.HasSuffix(t, ".local") {
		t += ".local"
	}
	return dns.Fqdn(t)
}

// serviceName is "Instance._type._tcp.local.".
func serviceName(instance, serviceType string) string {
	instance = strings.TrimSuffix(instance, ".")
	t := strings.TrimSuffix(normalize(serviceType), ".")
	return instance + "." + t + "."
}

// hostName is the machine's mDNS host name.
func hostName() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		h = "mygo"
	}
	h = strings.ReplaceAll(h, " ", "-")
	return h + ".local."
}

// browser collects responses for one service type.
type browser struct {
	engine *Engine
	kind   string
	found  func(Service)

	mu     sync.Mutex
	last   map[string]string
	byName map[string]*Service
	// expiry is when each service's records go stale. A browse forgets an
	// expired service so a later answer can report it fresh; without this a
	// device that left the network stayed listed for ever.
	expiry map[string]time.Time
}

// queryLoop sends a query at each step of the schedule and stops: a browse
// that kept asking would be the storm this package exists to avoid.
func (b *browser) queryLoop(ctx context.Context, stop chan struct{}) {
	for _, delay := range []time.Duration{0, time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second} {
		b.prune()
		select {
		case <-stop:
			return
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		if !b.engine.guard.allow() {
			continue
		}
		msg := new(dns.Msg)
		msg.SetQuestion(b.kind, dns.TypePTR)
		msg.RecursionDesired = false
		// Known-answer suppression: a query repeats what the browser already
		// knows, so devices that have nothing new stay quiet. Only records
		// with more than half their TTL left count.
		b.mu.Lock()
		now := time.Now()
		for key, svc := range b.byName {
			remaining := b.expiry[key].Sub(now)
			if remaining < ttl/2*time.Second {
				continue
			}
			msg.Answer = append(msg.Answer, &dns.PTR{
				Hdr: dns.RR_Header{Name: b.kind, Rrtype: dns.TypePTR, Class: dns.ClassINET, Ttl: uint32(remaining.Seconds())},
				Ptr: serviceName(svc.Instance, svc.Type),
			})
		}
		b.mu.Unlock()
		b.engine.write(msg)
	}
}

// absorb reads a response into the browser's services. Everything in the
// message is applied before anything is reported, so a PTR arriving before its
// SRV, or an A after both, still reaches the caller with what the packet said.
func (b *browser) absorb(msg *dns.Msg) {
	b.mu.Lock()
	touched := make(map[string]bool)
	// A cache-flush record replaces what this browser held for its name and
	// type; the first one of a packet does the clearing, later ones add.
	flushed := make(map[string]bool)
	wasFlushed := func(name string, t uint16) bool {
		key := fmt.Sprintf("%d|%s", t, strings.ToLower(name))
		if flushed[key] {
			return false
		}
		flushed[key] = true
		return true
	}
	for _, rr := range append(append([]dns.RR(nil), msg.Answer...), msg.Extra...) {
		switch v := rr.(type) {
		case *dns.PTR:
			if !strings.EqualFold(v.Hdr.Name, b.kind) {
				continue
			}
			key := strings.ToLower(v.Ptr)
			if b.byName[key] == nil {
				if len(b.byName) >= maxServices {
					continue
				}
				b.byName[key] = &Service{Instance: trimInstance(v.Ptr), Type: strings.TrimSuffix(b.kind, ".")}
			}
			b.noteTTL(key, v.Hdr.Ttl)
			touched[key] = true
		case *dns.SRV:
			key := strings.ToLower(v.Hdr.Name)
			svc := b.byName[key]
			if svc == nil {
				continue
			}
			if v.Hdr.Class&cacheFlush != 0 && wasFlushed(v.Hdr.Name, dns.TypeSRV) {
				svc.Port, svc.Host = 0, ""
			}
			svc.Port = int(v.Port)
			svc.Host = v.Target
			b.noteTTL(key, v.Hdr.Ttl)
			touched[key] = true
		case *dns.TXT:
			key := strings.ToLower(v.Hdr.Name)
			if svc := b.byName[key]; svc != nil {
				if v.Hdr.Class&cacheFlush != 0 && wasFlushed(v.Hdr.Name, dns.TypeTXT) {
					svc.TXT = nil
				}
				svc.TXT = append([]string(nil), v.Txt...)
				touched[key] = true
			}
		case *dns.A:
			for key, svc := range b.byName {
				if svc.Port <= 0 || !strings.EqualFold(svc.Host, v.Hdr.Name) {
					continue
				}
				if v.Hdr.Class&cacheFlush != 0 && wasFlushed(v.Hdr.Name, dns.TypeA) {
					svc.Addrs = dropFamily(svc.Addrs, 4)
				}
				if !hasIP(svc.Addrs, v.A) {
					svc.Addrs = append(svc.Addrs, v.A)
					touched[key] = true
				}
			}
		case *dns.AAAA:
			for key, svc := range b.byName {
				if svc.Port <= 0 || !strings.EqualFold(svc.Host, v.Hdr.Name) {
					continue
				}
				if v.Hdr.Class&cacheFlush != 0 && wasFlushed(v.Hdr.Name, dns.TypeAAAA) {
					svc.Addrs = dropFamily(svc.Addrs, 6)
				}
				if !hasIP(svc.Addrs, v.AAAA) {
					svc.Addrs = append(svc.Addrs, v.AAAA)
					touched[key] = true
				}
			}
		}
	}
	var emit []Service
	for key := range touched {
		svc := b.byName[key]
		if svc == nil || svc.Port <= 0 {
			continue
		}
		signature := fmt.Sprintf("%d|%v|%v", svc.Port, svc.Addrs, svc.TXT)
		if b.last[key] == signature {
			continue
		}
		b.last[key] = signature
		out := *svc
		out.Addrs = append([]net.IP(nil), svc.Addrs...)
		out.TXT = append([]string(nil), svc.TXT...)
		emit = append(emit, out)
	}
	b.mu.Unlock()
	// Outside the lock: a callback that cancels the browse takes it.
	for _, svc := range emit {
		b.found(svc)
	}
}

// noteTTL records when a service's records expire. b.mu is held.
func (b *browser) noteTTL(key string, seconds uint32) {
	if seconds == 0 {
		// A zero TTL is a goodbye: the device is leaving.
		b.expiry[key] = time.Now()
		return
	}
	at := time.Now().Add(time.Duration(seconds) * time.Second)
	if b.expiry[key].Before(at) {
		b.expiry[key] = at
	}
}

// prune forgets services whose records have expired, so a device that left
// the network disappears and one that came back is reported afresh. b.mu is
// not held.
func (b *browser) prune() {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	for key, at := range b.expiry {
		if now.After(at) {
			delete(b.byName, key)
			delete(b.last, key)
			delete(b.expiry, key)
		}
	}
}

// dropFamily removes the addresses of one family, for a cache-flush record
// that replaces them.
func dropFamily(addrs []net.IP, family int) []net.IP {
	// A new slice, not addrs[:0]: callers keep the original list (and the
	// test that calls this twice would otherwise see the first call's result).
	out := make([]net.IP, 0, len(addrs))
	for _, ip := range addrs {
		is4 := ip.To4() != nil
		if (family == 4) == is4 {
			continue
		}
		out = append(out, ip)
	}
	return out
}

// hasIP reports whether addrs already holds ip: a repeated announcement must
// not grow the list the caller sees.
func hasIP(addrs []net.IP, ip net.IP) bool {
	for _, a := range addrs {
		if a.Equal(ip) {
			return true
		}
	}
	return false
}

// localAddrs is this device's IPv4 addresses, for a service advertised
// without any: a peer needs one to connect.
func localAddrs() []net.IP {
	var out []net.IP
	add := func(ip net.IP) {
		// Loopback, link-local and unspecified address a peer cannot use; a
		// link-local IPv6 would also need a zone this record cannot carry.
		if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
			return
		}
		if !hasIP(out, ip) {
			out = append(out, ip)
		}
	}
	// The address the multicast route picks is the one a peer on this network
	// can use; asking the route table needs no interface walk. Dialing UDP
	// sends nothing.
	if conn, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353}); err == nil {
		addr := conn.LocalAddr()
		_ = conn.Close()
		if a, ok := addr.(*net.UDPAddr); ok {
			add(a.IP)
		}
	}
	// The rest of the host's addresses, IPv4 and IPv6, for networks where the
	// route above is not the only way in.
	ifaces, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok {
				add(ipnet.IP)
			}
		}
	}
	return out
}

// trimInstance turns "Office._http._tcp.local." into "Office".
func trimInstance(name string) string {
	n := strings.TrimSuffix(name, ".")
	parts := strings.SplitN(n, ".", 2)
	return parts[0]
}
