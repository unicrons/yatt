package scan_test

import (
	"context"
	"net"
	"testing"

	"github.com/miekg/dns"

	"github.com/andoniaf/yatt/internal/scan"
	"github.com/andoniaf/yatt/pkg/engine"
)

// alwaysNXDOMAIN answers every query NXDOMAIN, so a run over it is fast and
// its finding count is driven entirely by how many candidates were
// generated, not by anything DNS-shaped.
type alwaysNXDOMAIN struct{}

func (alwaysNXDOMAIN) Query(_ context.Context, name string, qtype uint16) (*dns.Msg, error) {
	msg := &dns.Msg{}
	msg.SetQuestion(dns.Fqdn(name), qtype)
	msg.Rcode = dns.RcodeNameError
	return msg, nil
}

func TestRunDefaultsToEveryRegisteredTechnique(t *testing.T) {
	t.Parallel()

	result, err := scan.Run(context.Background(), scan.Options{
		Seed:     "example.com",
		Resolver: alwaysNXDOMAIN{},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	seen := make(map[string]bool)
	for _, f := range result.Findings {
		seen[f.Technique] = true
	}
	for _, name := range engine.Names() {
		if !seen[name] {
			t.Errorf("default run produced no %q candidates", name)
		}
	}
}

func TestRunHonoursExplicitTechniques(t *testing.T) {
	t.Parallel()

	result, err := scan.Run(context.Background(), scan.Options{
		Seed:       "example.com",
		Resolver:   alwaysNXDOMAIN{},
		Techniques: []engine.Technique{engine.Omission{}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	for _, f := range result.Findings {
		if f.IsOriginal() {
			continue
		}
		if f.Technique != "omission" {
			t.Errorf("finding %q has technique %q, want only omission", f.Candidate, f.Technique)
		}
	}
}

func TestRunTLDProfileFullExpandsTheCandidateCount(t *testing.T) {
	t.Parallel()

	common, err := scan.Run(context.Background(), scan.Options{
		Seed:       "example.com",
		Resolver:   alwaysNXDOMAIN{},
		Techniques: []engine.Technique{engine.TLD{}},
	})
	if err != nil {
		t.Fatalf("Run (common): %v", err)
	}

	full, err := scan.Run(context.Background(), scan.Options{
		Seed:       "example.com",
		Resolver:   alwaysNXDOMAIN{},
		Techniques: []engine.Technique{engine.TLD{}},
		TLDProfile: engine.TLDProfileFull,
	})
	if err != nil {
		t.Fatalf("Run (full): %v", err)
	}

	if len(full.Findings) <= len(common.Findings) {
		t.Errorf("full profile produced %d findings, want more than common's %d",
			len(full.Findings), len(common.Findings))
	}
}

func TestRunTLDsOverridesTheProfile(t *testing.T) {
	t.Parallel()

	result, err := scan.Run(context.Background(), scan.Options{
		Seed:       "example.com",
		Resolver:   alwaysNXDOMAIN{},
		Techniques: []engine.Technique{engine.TLD{}},
		TLDProfile: engine.TLDProfileFull,
		TLDs:       []string{"net", "org"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// The seed's own row plus exactly the two custom TLDs — the "full"
	// profile named alongside TLDs must be ignored.
	if len(result.Findings) != 3 {
		t.Fatalf("got %d findings, want 3 (seed + net + org): %+v", len(result.Findings), result.Findings)
	}
}

func TestRunLimitBoundsTheCandidateCount(t *testing.T) {
	t.Parallel()

	result, err := scan.Run(context.Background(), scan.Options{
		Seed:       "example.com",
		Resolver:   alwaysNXDOMAIN{},
		Techniques: []engine.Technique{engine.Omission{}},
		Limit:      3,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(result.Findings) != 4 { // the seed's own row, plus 3 candidates
		t.Fatalf("got %d findings, want 4 (the seed plus 3 candidates)", len(result.Findings))
	}
	if !result.Findings[0].IsOriginal() {
		t.Errorf("first finding = %+v, want the seed's own row", result.Findings[0])
	}
}

func TestRunFullTLDSweepIsNotTruncated(t *testing.T) {
	t.Parallel()

	result, err := scan.Run(context.Background(), scan.Options{
		Seed:       "example.com",
		Resolver:   alwaysNXDOMAIN{},
		Techniques: []engine.Technique{engine.TLD{}},
		TLDProfile: engine.TLDProfileFull,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// The whole point of `--tld-profile full` is sweeping the whole IANA list
	// (well over a thousand TLDs), so the per-technique cap must not apply to
	// TLD swap: capped, the sweep would silently stop in the alphabet around
	// "g" while claiming full coverage.
	if len(result.Findings) <= engine.DefaultTechniqueCap+1 {
		t.Errorf("got %d findings, want more than %d — the full sweep must not be cut to the per-technique cap",
			len(result.Findings), engine.DefaultTechniqueCap+1)
	}
}

func TestRunLimitStillBoundsAFullTLDSweep(t *testing.T) {
	t.Parallel()

	result, err := scan.Run(context.Background(), scan.Options{
		Seed:       "example.com",
		Resolver:   alwaysNXDOMAIN{},
		Techniques: []engine.Technique{engine.TLD{}},
		TLDProfile: engine.TLDProfileFull,
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Exempting TLD swap from the per-technique cap must not exempt it from
	// the explicit --limit: that one the user asked for.
	if len(result.Findings) != 11 { // the seed's own row, plus 10 candidates
		t.Fatalf("got %d findings, want 11 (the seed plus the --limit of 10)", len(result.Findings))
	}
}

// TestRunRejectsAnUnknownTLDProfile checks the error surfaces through Run
// rather than being silently swallowed.
func TestRunRejectsAnUnknownTLDProfile(t *testing.T) {
	t.Parallel()

	_, err := scan.Run(context.Background(), scan.Options{
		Seed:       "example.com",
		Resolver:   alwaysNXDOMAIN{},
		Techniques: []engine.Technique{engine.TLD{}},
		TLDProfile: "bogus",
	})
	if err == nil {
		t.Fatal("Run with an unknown TLD profile succeeded, want an error")
	}
}

// ipResolver answers A queries for the given names with a fixed IP, so a
// resolver-shaped test can assert something actually resolved through a
// homoglyph candidate's punycode form.
type ipResolver struct {
	registered map[string]net.IP
}

func (r ipResolver) Query(_ context.Context, name string, qtype uint16) (*dns.Msg, error) {
	msg := &dns.Msg{}
	msg.SetQuestion(dns.Fqdn(name), qtype)

	ip, ok := r.registered[dns.Fqdn(name)]
	if !ok {
		msg.Rcode = dns.RcodeNameError
		return msg, nil
	}
	msg.Rcode = dns.RcodeSuccess
	switch qtype {
	case dns.TypeNS:
		msg.Answer = append(msg.Answer, &dns.NS{
			Hdr: dns.RR_Header{Name: dns.Fqdn(name), Rrtype: dns.TypeNS},
			Ns:  "ns1.example.net.",
		})
	case dns.TypeA:
		msg.Answer = append(msg.Answer, &dns.A{
			Hdr: dns.RR_Header{Name: dns.Fqdn(name), Rrtype: dns.TypeA},
			A:   ip,
		})
	}
	return msg, nil
}

// TestRunResolvesHomoglyphCandidatesByTheirPunycodeForm is the end-to-end
// guard on idn.go: a homoglyph candidate must reach the resolver in its
// "xn--" wire form, not as raw Unicode, or a registered look-alike would
// never be found registered.
func TestRunResolvesHomoglyphCandidatesByTheirPunycodeForm(t *testing.T) {
	t.Parallel()

	const punycode = "xn--pple-43d.com" // Cyrillic "а" + "pple", punycode form
	result, err := scan.Run(context.Background(), scan.Options{
		Seed:       "apple.com",
		Resolver:   ipResolver{registered: map[string]net.IP{dns.Fqdn(punycode): net.ParseIP("192.0.2.1")}},
		Techniques: []engine.Technique{engine.Homoglyph{}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var found bool
	for _, f := range result.Findings {
		if f.Candidate == punycode {
			found = true
			if !f.Registered {
				t.Errorf("candidate %q reported unregistered, want registered", punycode)
			}
		}
		// No raw Unicode candidate may reach the report — every homoglyph
		// candidate must already be in its DNS wire form.
		for _, r := range f.Candidate {
			if r > 127 {
				t.Errorf("candidate %q contains a non-ASCII rune, want the punycode form", f.Candidate)
				break
			}
		}
	}
	if !found {
		t.Fatalf("homoglyph technique never produced %q", punycode)
	}
}
