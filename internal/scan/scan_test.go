package scan_test

import (
	"context"
	"encoding/json"
	"math/rand"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/andoniaf/yatt/internal/scan"
	"github.com/andoniaf/yatt/pkg/engine"
)

// jitteryResolver answers deterministically by name but takes a randomly
// varying amount of time to do it.
//
// The jitter is the point. Concurrent workers finishing in a different order on
// every run is exactly the condition that would let completion order leak into
// the report, and a fake that answers instantly would almost always complete in
// submission order and hide the bug.
type jitteryResolver struct {
	mu      sync.Mutex
	rng     *rand.Rand
	queried []string
}

func newJitteryResolver() *jitteryResolver {
	return &jitteryResolver{rng: rand.New(rand.NewSource(1))} //nolint:gosec // not cryptographic
}

func (r *jitteryResolver) Query(ctx context.Context, name string, qtype uint16) (*dns.Msg, error) {
	r.mu.Lock()
	delay := time.Duration(r.rng.Intn(400)) * time.Microsecond
	r.queried = append(r.queried, name)
	r.mu.Unlock()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(delay):
	}

	msg := &dns.Msg{}
	msg.SetQuestion(dns.Fqdn(name), qtype)

	// Half the candidates resolve, chosen by a stable property of the name, so
	// the answer for a given name never varies between runs.
	if len(strings.TrimSuffix(name, "."))%2 == 0 {
		msg.Rcode = dns.RcodeNameError
		return msg, nil
	}

	msg.Rcode = dns.RcodeSuccess
	switch qtype {
	case dns.TypeA:
		msg.Answer = append(msg.Answer, &dns.A{
			Hdr: dns.RR_Header{Name: dns.Fqdn(name), Rrtype: dns.TypeA},
			A:   net.ParseIP("192.0.2.1"),
		})
	case dns.TypeNS:
		msg.Answer = append(msg.Answer, &dns.NS{
			Hdr: dns.RR_Header{Name: dns.Fqdn(name), Rrtype: dns.TypeNS},
			Ns:  "ns1.example.net.",
		})
	}
	return msg, nil
}

// TestConcurrentRunsAreDeterministic is the guard on the whole diff feature.
//
// Cross-scan diffing compares candidate to candidate, so a report whose row
// order depended on which DNS answer arrived first would produce spurious
// new/gone churn on every run even when nothing about the domains changed.
// Comparing serialised output rather than field-by-field is deliberate: it
// catches ordering, and it catches any future field that acquires a
// non-deterministic value.
func TestConcurrentRunsAreDeterministic(t *testing.T) {
	t.Parallel()

	var first string
	for run := 0; run < 8; run++ {
		result, err := scan.Run(context.Background(), scan.Options{
			Seed:        "example.com",
			Resolver:    newJitteryResolver(),
			Concurrency: 16,
		})
		if err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		if len(result.Findings) < 2 {
			t.Fatalf("run %d produced %d findings, want the seed plus candidates",
				run, len(result.Findings))
		}

		encoded, err := json.Marshal(result.Findings)
		if err != nil {
			t.Fatalf("run %d: marshal: %v", run, err)
		}
		if run == 0 {
			first = string(encoded)
			continue
		}
		if string(encoded) != first {
			t.Fatalf("run %d differs from run 0 — concurrent resolution leaked into the report:\n first: %s\n got:   %s",
				run, first, encoded)
		}
	}
}

// TestConcurrentRunKeepsTheSeedFirst guards the interaction between the seed
// row and concurrency: the seed is submitted first but need not finish first.
func TestConcurrentRunKeepsTheSeedFirst(t *testing.T) {
	t.Parallel()

	for run := 0; run < 8; run++ {
		result, err := scan.Run(context.Background(), scan.Options{
			Seed:        "example.com",
			Resolver:    newJitteryResolver(),
			Concurrency: 16,
		})
		if err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		if got := result.Findings[0]; got.Technique != engine.TechniqueOriginal || got.Candidate != "example.com" {
			t.Fatalf("run %d: first finding = %q/%q, want the seed row",
				run, got.Candidate, got.Technique)
		}
	}
}

// TestConcurrencyIsBounded checks that the limit is actually applied, since an
// unbounded errgroup passes every correctness test while melting a resolver.
func TestConcurrencyIsBounded(t *testing.T) {
	t.Parallel()

	const limit = 3
	var (
		mu       sync.Mutex
		inFlight int
		peak     int
	)

	counting := resolverFunc(func(ctx context.Context, name string, qtype uint16) (*dns.Msg, error) {
		mu.Lock()
		inFlight++
		if inFlight > peak {
			peak = inFlight
		}
		mu.Unlock()

		time.Sleep(time.Millisecond)

		mu.Lock()
		inFlight--
		mu.Unlock()

		msg := &dns.Msg{}
		msg.SetQuestion(dns.Fqdn(name), qtype)
		msg.Rcode = dns.RcodeNameError
		return msg, nil
	})

	if _, err := scan.Run(context.Background(), scan.Options{
		Seed:        "example.com",
		Resolver:    counting,
		Concurrency: limit,
	}); err != nil {
		t.Fatalf("run: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if peak > limit {
		t.Errorf("peak concurrent queries = %d, want at most %d", peak, limit)
	}
}

// resolverFunc adapts a function to the resolver interface.
type resolverFunc func(ctx context.Context, name string, qtype uint16) (*dns.Msg, error)

func (f resolverFunc) Query(ctx context.Context, name string, qtype uint16) (*dns.Msg, error) {
	return f(ctx, name, qtype)
}
