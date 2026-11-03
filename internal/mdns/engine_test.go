package mdns

import (
	"net"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// A query that already carries a fresh PTR for the instance needs no answer;
// one with a stale TTL, or for another instance, still does.
func TestKnownAnswer(t *testing.T) {
	build := func(ttl uint32, target string) *dns.Msg {
		msg := new(dns.Msg)
		msg.SetQuestion("_http._tcp.local.", dns.TypePTR)
		if target != "" {
			msg.Answer = append(msg.Answer, &dns.PTR{
				Hdr: dns.RR_Header{Name: "_http._tcp.local.", Rrtype: dns.TypePTR, Class: dns.ClassINET, Ttl: ttl},
				Ptr: target,
			})
		}
		return msg
	}
	if !knownAnswer(build(120, "Office._http._tcp.local."), "office._http._tcp.local.") {
		t.Error("a fresh known answer should suppress the reply")
	}
	if knownAnswer(build(10, "Office._http._tcp.local."), "Office._http._tcp.local.") {
		t.Error("a nearly expired answer should not suppress the reply")
	}
	if knownAnswer(build(120, "Other._http._tcp.local."), "Office._http._tcp.local.") {
		t.Error("another instance's answer should not suppress the reply")
	}
	if knownAnswer(build(0, ""), "Office._http._tcp.local.") {
		t.Error("a query with no answers should not suppress the reply")
	}
}

// A service's expiry is the later of what its records say, a zero TTL expires
// it at once, and prune removes what has expired.
func TestBrowserExpiry(t *testing.T) {
	b := &browser{last: map[string]string{}, byName: map[string]*Service{"svc": {}}, expiry: map[string]time.Time{}}
	b.noteTTL("svc", 120)
	first := b.expiry["svc"]
	if time.Until(first) < 100*time.Second {
		t.Fatalf("TTL 120 became %v", time.Until(first))
	}
	b.noteTTL("svc", 10)
	if !b.expiry["svc"].Equal(first) {
		t.Error("a shorter TTL replaced the later expiry")
	}
	b.noteTTL("svc", 0)
	b.prune()
	if _, ok := b.byName["svc"]; ok {
		t.Error("a zero TTL should expire the service")
	}
	if _, ok := b.expiry["svc"]; ok {
		t.Error("prune should forget the expiry too")
	}
}

// Addresses are kept once, in either family.
func TestHasIP(t *testing.T) {
	addrs := []net.IP{net.IPv4(10, 0, 2, 16)}
	if !hasIP(addrs, net.IPv4(10, 0, 2, 16)) {
		t.Error("a repeated IPv4 address was not seen")
	}
	if hasIP(addrs, net.IPv4(10, 0, 2, 17)) {
		t.Error("a different IPv4 address was seen")
	}
	if hasIP(addrs, net.ParseIP("fe80::1")) {
		t.Error("an IPv6 address matched an IPv4 one")
	}
}

// Another host claiming our instance name with a different port makes us
// advertise under a numbered name; our own records do not.
func TestConflictRenames(t *testing.T) {
	e := &Engine{renames: map[string]int{}, guard: newGuard()}
	svc := Service{Instance: "Office", Type: "_http._tcp", Port: 8080, Host: "me.local."}
	e.adverts = append(e.adverts, &svc)

	other := new(dns.Msg)
	other.Response = true
	other.Answer = append(other.Answer, &dns.SRV{
		Hdr:    dns.RR_Header{Name: "Office._http._tcp.local.", Rrtype: dns.TypeSRV, Class: dns.ClassINET | cacheFlush, Ttl: ttl},
		Port:   9999,
		Target: "other.local.",
	})
	e.checkConflict(other)
	if svc.Instance != "Office (2)" {
		t.Fatalf("instance = %q, want Office (2)", svc.Instance)
	}

	ours := new(dns.Msg)
	ours.Response = true
	ours.Answer = append(ours.Answer, &dns.SRV{
		Hdr:    dns.RR_Header{Name: serviceName(svc.Instance, svc.Type), Rrtype: dns.TypeSRV, Class: dns.ClassINET | cacheFlush, Ttl: ttl},
		Port:   8080,
		Target: "me.local.",
	})
	e.checkConflict(ours)
	if svc.Instance != "Office (2)" {
		t.Fatalf("our own record renamed us to %q", svc.Instance)
	}
	if got, want := renamedInstance("Office", 2), "Office (2)"; got != want {
		t.Errorf("renamedInstance = %q, want %q", got, want)
	}
}

// PTR is shared and must not carry the cache-flush bit; SRV, TXT and A are
// unique to this host and must.
func TestRecordsCacheFlush(t *testing.T) {
	for _, rr := range records(Service{
		Instance: "Office",
		Type:     "_http._tcp",
		Port:     8080,
		Addrs:    []net.IP{net.IPv4(10, 0, 2, 16)},
		TXT:      []string{"v=1"},
	}) {
		hdr := rr.Header()
		shared := hdr.Rrtype == dns.TypePTR
		has := hdr.Class&cacheFlush != 0
		if has == shared {
			t.Errorf("%s cache-flush = %v, shared = %v", dns.TypeToString[hdr.Rrtype], has, shared)
		}
		if hdr.Class&^cacheFlush != dns.ClassINET {
			t.Errorf("%s class = %#x", dns.TypeToString[hdr.Rrtype], hdr.Class)
		}
	}
}

// A cache-flush address record replaces the addresses of its family only.
func TestDropFamily(t *testing.T) {
	addrs := []net.IP{net.IPv4(10, 0, 2, 16), net.ParseIP("2001:db8::1")}
	if got := dropFamily(addrs, 4); len(got) != 1 || got[0].To4() != nil {
		t.Errorf("dropFamily(4) = %v, want only the IPv6 address", got)
	}
	if got := dropFamily(addrs, 6); len(got) != 1 || got[0].To4() == nil {
		t.Errorf("dropFamily(6) = %v, want only the IPv4 address", got)
	}
}

// The service type is normalized the same way for browsing and advertising.
func TestNormalize(t *testing.T) {
	for _, in := range []string{"_http._tcp", "_http._tcp.local", "_http._tcp.local."} {
		if got, want := normalize(in), "_http._tcp.local."; got != want {
			t.Errorf("normalize(%q) = %q, want %q", in, got, want)
		}
	}
	if got, want := serviceName("Office", "_http._tcp"), "Office._http._tcp.local."; got != want {
		t.Errorf("serviceName = %q, want %q", got, want)
	}
}
