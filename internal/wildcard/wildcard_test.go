package wildcard_test

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"

	"github.com/miekg/dns"

	"github.com/andoniaf/yatt/internal/wildcard"
)

// catchAllResolver answers every A query under a zone with the same addresses,
// which is what a registry wildcard looks like from the outside.
type catchAllResolver struct {
	// zones maps a zone to the addresses it hands out for any name under it.
	zones map[string][]string
	// err, when set, fails every query.
	err error

	mu    sync.Mutex
	asked []string
}

func (r *catchAllResolver) Query(_ context.Context, name string, qtype uint16) (*dns.Msg, error) {
	r.mu.Lock()
	r.asked = append(r.asked, strings.TrimSuffix(dns.Fqdn(name), "."))
	r.mu.Unlock()

	if r.err != nil {
		return nil, r.err
	}

	m := new(dns.Msg)
	if qtype != dns.TypeA {
		// Only A is scripted; AAAA answers NOERROR-empty, which is the ordinary
		// case for the zones this matters for.
		m.Rcode = dns.RcodeSuccess
		return m, nil
	}

	trimmed := strings.TrimSuffix(dns.Fqdn(name), ".")
	for zone, addresses := range r.zones {
		if !strings.HasSuffix(trimmed, "."+zone) {
			continue
		}
		m.Rcode = dns.RcodeSuccess
		for _, addr := range addresses {
			m.Answer = append(m.Answer, &dns.A{
				Hdr: dns.RR_Header{Name: dns.Fqdn(name), Rrtype: dns.TypeA},
				A:   mustIP(addr),
			})
		}
		return m, nil
	}
	m.Rcode = dns.RcodeNameError
	return m, nil
}

// queriesFor counts how many queries were issued for names under a zone.
func (r *catchAllResolver) queriesFor(zone string) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	var n int
	for _, name := range r.asked {
		if strings.HasSuffix(name, "."+zone) {
			n++
		}
	}
	return n
}

func TestSignatureDetectsCatchAllZone(t *testing.T) {
	fake := &catchAllResolver{zones: map[string][]string{"example": {"192.0.2.1"}}}
	detector := wildcard.New(fake)

	sig, err := detector.Signature(context.Background(), "example")
	if err != nil {
		t.Fatalf("Signature: %v", err)
	}

	if !sig.Wildcard() {
		t.Fatalf("Wildcard() = false, want true for a zone that answers every name")
	}
	if len(sig.Addresses) != 1 || sig.Addresses[0] != "192.0.2.1" {
		t.Errorf("Addresses = %v, want [192.0.2.1]", sig.Addresses)
	}
}

func TestSignatureOfAnHonestZoneIsEmpty(t *testing.T) {
	// The ordinary case: nonexistent names get NXDOMAIN, so there is no baseline
	// to subtract and every candidate's answer means what it says.
	fake := &catchAllResolver{}
	detector := wildcard.New(fake)

	sig, err := detector.Signature(context.Background(), "com")
	if err != nil {
		t.Fatalf("Signature: %v", err)
	}
	if sig.Wildcard() {
		t.Errorf("Wildcard() = true with addresses %v, want false", sig.Addresses)
	}
}

func TestSignatureIgnoresUnstableAnswers(t *testing.T) {
	// A zone that answers with a different address every time has no stable
	// baseline. Treating the first answer as the signature would suppress
	// whichever candidate happened to land on it.
	answers := []string{"192.0.2.1", "192.0.2.2", "192.0.2.3"}
	var probe int
	var mu sync.Mutex
	rotating := resolverFunc(func(_ context.Context, name string, qtype uint16) (*dns.Msg, error) {
		m := new(dns.Msg)
		m.Rcode = dns.RcodeSuccess
		if qtype != dns.TypeA {
			return m, nil
		}
		mu.Lock()
		addr := answers[probe%len(answers)]
		probe++
		mu.Unlock()
		m.Answer = append(m.Answer, &dns.A{
			Hdr: dns.RR_Header{Name: dns.Fqdn(name), Rrtype: dns.TypeA},
			A:   mustIP(addr),
		})
		return m, nil
	})

	sig, err := wildcard.New(rotating).Signature(context.Background(), "example")
	if err != nil {
		t.Fatalf("Signature: %v", err)
	}
	if sig.Wildcard() {
		t.Errorf("Wildcard() = true with addresses %v, want false: no answer recurred", sig.Addresses)
	}
}

func TestSignatureProbesEachZoneOnce(t *testing.T) {
	// The cache is what makes this affordable: a scan puts thousands of
	// candidates under one zone, and re-probing per candidate would multiply the
	// query budget by the probe count for an answer that cannot have changed.
	fake := &catchAllResolver{zones: map[string][]string{"example": {"192.0.2.1"}}}
	detector := wildcard.New(fake)

	for i := 0; i < 50; i++ {
		if _, err := detector.Signature(context.Background(), "example"); err != nil {
			t.Fatalf("Signature: %v", err)
		}
	}

	// Two queries (A and AAAA) per probe.
	if got, want := fake.queriesFor("example"), wildcard.DefaultProbes*2; got != want {
		t.Errorf("issued %d queries for the zone, want %d: the signature must be cached", got, want)
	}
}

func TestSignatureProbesOnceUnderConcurrency(t *testing.T) {
	// Serial caching is not enough: the scan asks from many workers at once, so
	// concurrent callers have to wait for the in-flight probe rather than start
	// their own.
	fake := &catchAllResolver{zones: map[string][]string{"example": {"192.0.2.1"}}}
	detector := wildcard.New(fake)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := detector.Signature(context.Background(), "example"); err != nil {
				t.Errorf("Signature: %v", err)
			}
		}()
	}
	wg.Wait()

	if got, want := fake.queriesFor("example"), wildcard.DefaultProbes*2; got != want {
		t.Errorf("issued %d queries for the zone, want %d", got, want)
	}
}

func TestSignatureCachesPerZone(t *testing.T) {
	fake := &catchAllResolver{zones: map[string][]string{"example": {"192.0.2.1"}}}
	detector := wildcard.New(fake)

	catchAll, err := detector.Signature(context.Background(), "example")
	if err != nil {
		t.Fatalf("Signature: %v", err)
	}
	honest, err := detector.Signature(context.Background(), "test")
	if err != nil {
		t.Fatalf("Signature: %v", err)
	}

	if !catchAll.Wildcard() {
		t.Error("the catch-all zone was not detected")
	}
	if honest.Wildcard() {
		t.Errorf("the honest zone got signature %v, want none: zones must not share a cache entry", honest.Addresses)
	}
}

func TestSignatureNormalizesTheZone(t *testing.T) {
	fake := &catchAllResolver{zones: map[string][]string{"example": {"192.0.2.1"}}}
	detector := wildcard.New(fake)

	if _, err := detector.Signature(context.Background(), "example"); err != nil {
		t.Fatalf("Signature: %v", err)
	}
	if _, err := detector.Signature(context.Background(), "EXAMPLE."); err != nil {
		t.Fatalf("Signature: %v", err)
	}

	if got, want := fake.queriesFor("example"), wildcard.DefaultProbes*2; got != want {
		t.Errorf("issued %d queries, want %d: \"EXAMPLE.\" and \"example\" are one zone", got, want)
	}
}

func TestSignaturePropagatesResolverErrors(t *testing.T) {
	fake := &catchAllResolver{err: errors.New("i/o timeout")}

	if _, err := wildcard.New(fake).Signature(context.Background(), "example"); err == nil {
		t.Fatal("Signature succeeded, want the resolver error")
	}
}

func TestIsReal(t *testing.T) {
	catchAll := wildcard.Signature{Zone: "example", Addresses: []string{"192.0.2.1", "192.0.2.2"}}
	none := wildcard.Signature{Zone: "com"}

	tests := []struct {
		name      string
		signature wildcard.Signature
		addresses []string
		want      bool
	}{
		{
			name:      "no wildcard means every answer is real",
			signature: none,
			addresses: []string{"192.0.2.1"},
			want:      true,
		},
		{
			name:      "an answer identical to the baseline is not real",
			signature: catchAll,
			addresses: []string{"192.0.2.1"},
			want:      false,
		},
		{
			name:      "the full baseline is not real either",
			signature: catchAll,
			addresses: []string{"192.0.2.1", "192.0.2.2"},
			want:      false,
		},
		{
			// Someone configured that extra address; the catch-all cannot account
			// for it, so the candidate is a real registration.
			name:      "one address outside the baseline makes it real",
			signature: catchAll,
			addresses: []string{"192.0.2.1", "198.51.100.7"},
			want:      true,
		},
		{
			// There is nothing here for the catch-all to explain away, and
			// suppressing it would hide a finding rather than a duplicate — a
			// registered-but-not-resolving candidate is exactly what this tool
			// exists to surface.
			name:      "a candidate with no addresses is not suppressed",
			signature: catchAll,
			addresses: nil,
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.signature.IsReal(tt.addresses); got != tt.want {
				t.Errorf("IsReal(%v) = %v, want %v", tt.addresses, got, tt.want)
			}
		})
	}
}

// resolverFunc adapts a function to the resolver interface.
type resolverFunc func(ctx context.Context, name string, qtype uint16) (*dns.Msg, error)

func (f resolverFunc) Query(ctx context.Context, name string, qtype uint16) (*dns.Msg, error) {
	return f(ctx, name, qtype)
}

// mustIP parses a dotted-quad literal for use in a scripted answer.
//
// It panics rather than returning an error because every address in these tests
// is a hard-coded literal: a failure here is a typo in the test, not a condition
// under test.
func mustIP(addr string) net.IP {
	ip := net.ParseIP(addr)
	if ip == nil {
		panic("wildcard_test: malformed test address " + addr)
	}
	return ip
}
