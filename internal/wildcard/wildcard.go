// Package wildcard detects catch-all DNS zones.
//
// A zone that answers every name — a registry wildcard like VeriSign's 2003
// Site Finder, a parking operator's catch-all, or a misconfigured
// authoritative server — makes the two signals this tool reports meaningless
// underneath it: every candidate looks registered and every candidate resolves
// to the same address. Without this check a TLD-swap scan is almost entirely
// noise.
//
// The technique is the established one (OWASP Amass' shape): ask for a random,
// virtually-certainly-nonexistent label under the zone several times, intersect
// the answers, and treat what survives as the zone's baseline. A candidate
// whose addresses are all in that baseline told us nothing the baseline had not
// already.
package wildcard

import (
	"context"
	"crypto/rand"
	"math/big"
	"sort"
	"strings"
	"sync"

	"github.com/miekg/dns"

	"github.com/andoniaf/yatt/internal/resolver"
)

// DefaultProbes is how many random labels are asked for per zone.
//
// One probe is not enough: a single coincidental answer — a transient
// resolver error page, an unlucky collision with a name that really exists —
// would condemn the whole zone. Intersecting several probes means only answers
// that recur count, which is what distinguishes a catch-all from an accident.
const DefaultProbes = 3

// labelLength is the length of a probe label. Long enough that a collision with
// a registered name is not a practical concern.
const labelLength = 16

// Signature is a zone's catch-all baseline: the addresses it returns for names
// that do not exist.
type Signature struct {
	// Zone is the name that was probed.
	Zone string
	// Addresses are the addresses every probe agreed on, sorted. Empty means the
	// zone is not a catch-all.
	Addresses []string
	// SuppressesNXDOMAIN reports that the zone answered NOERROR for a name
	// that does not exist. Under such a zone an rcode-based registration test
	// is meaningless: "not NXDOMAIN" is what every name gets, registered or
	// not. Some registries (.gov, .ph, .fm) answer this way without handing
	// out addresses, so this is independent of Wildcard().
	SuppressesNXDOMAIN bool
}

// Wildcard reports whether the zone answers for names that do not exist.
func (s Signature) Wildcard() bool { return len(s.Addresses) > 0 }

// IsReal reports whether a candidate's addresses say anything the zone's
// catch-all had not already said.
//
// A candidate is real when it resolves to at least one address the wildcard
// does not hand out — matching the wildcard only partially still means someone
// configured something. A candidate with no addresses at all is also real:
// there is nothing there that the catch-all could account for, so suppressing
// it would hide a finding rather than a duplicate.
func (s Signature) IsReal(candidateIPs []string) bool {
	if !s.Wildcard() || len(candidateIPs) == 0 {
		return true
	}
	baseline := make(map[string]bool, len(s.Addresses))
	for _, addr := range s.Addresses {
		baseline[addr] = true
	}
	for _, addr := range candidateIPs {
		if !baseline[addr] {
			return true
		}
	}
	return false
}

// Detector resolves and caches one signature per zone.
//
// The cache is the point: a scan of a single seed puts every candidate under
// the same handful of zones, and re-probing per candidate would multiply the
// query budget by the probe count for an answer that cannot have changed
// mid-scan.
type Detector struct {
	resolver resolver.Resolver
	probes   int

	mu    sync.Mutex
	zones map[string]*entry
}

// entry is one zone's in-flight or completed probe.
//
// Concurrent callers wait on ready rather than probing in parallel, so "probed
// once per zone" holds under the bounded fan-out the scan runs at, not just
// serially. A failed probe is cached like a successful one: a Detector lives
// for one scan, and a resolver that could not answer three probes is not going
// to answer better for the next thousand candidates in the same zone.
type entry struct {
	ready chan struct{}
	sig   Signature
	err   error
}

// New returns a Detector probing through r.
func New(r resolver.Resolver) *Detector {
	return &Detector{resolver: r, probes: DefaultProbes, zones: map[string]*entry{}}
}

// Signature returns the zone's catch-all baseline, probing it on first ask and
// serving the cached answer afterwards.
func (d *Detector) Signature(ctx context.Context, zone string) (Signature, error) {
	zone = normalizeZone(zone)
	if zone == "" {
		return Signature{}, nil
	}

	d.mu.Lock()
	cached, ok := d.zones[zone]
	if !ok {
		cached = &entry{ready: make(chan struct{})}
		d.zones[zone] = cached
	}
	d.mu.Unlock()

	if ok {
		select {
		case <-ctx.Done():
			return Signature{}, ctx.Err()
		case <-cached.ready:
		}
		return cached.sig, cached.err
	}

	cached.sig, cached.err = d.probe(ctx, zone)
	close(cached.ready)
	return cached.sig, cached.err
}

// probe asks for several random labels under the zone and intersects the
// answers.
func (d *Detector) probe(ctx context.Context, zone string) (Signature, error) {
	sig := Signature{Zone: zone}

	var common map[string]bool
	for i := 0; i < d.probes; i++ {
		label, err := randomLabel()
		if err != nil {
			return sig, err
		}
		addresses, aNoError, err := d.addresses(ctx, label+"."+zone)
		if err != nil {
			return sig, err
		}
		if i == 0 {
			// One probe settles it either way: a zone either signals
			// non-existence with NXDOMAIN or it does not.
			sig.SuppressesNXDOMAIN = aNoError
		}
		if len(addresses) == 0 {
			// One honest NXDOMAIN settles it: a zone that lets a nonexistent
			// name be nonexistent is not a catch-all, and the remaining probes
			// cannot change that.
			return sig, nil
		}
		if common == nil {
			common = addresses
			continue
		}
		for addr := range common {
			if !addresses[addr] {
				delete(common, addr)
			}
		}
		if len(common) == 0 {
			// The probes answered, but never with the same address twice. That
			// is churn, not a stable baseline worth subtracting.
			return sig, nil
		}
	}

	sig.Addresses = make([]string, 0, len(common))
	for addr := range common {
		sig.Addresses = append(sig.Addresses, addr)
	}
	// Sorted so a signature — and anything derived from it — reads the same on
	// every run, whatever order the resolver listed its answers in.
	sort.Strings(sig.Addresses)
	return sig, nil
}

// addresses resolves a name's A and AAAA records into a set. It also reports
// whether the A response was NOERROR: only the A rcode can tell an
// NXDOMAIN-suppressing zone from an honest one, because an honest zone
// answers AAAA with NOERROR-empty for any v4-only name.
func (d *Detector) addresses(ctx context.Context, name string) (map[string]bool, bool, error) {
	out := map[string]bool{}
	var aNoError bool
	for _, qtype := range []uint16{dns.TypeA, dns.TypeAAAA} {
		resp, err := d.resolver.Query(ctx, name, qtype)
		if err != nil {
			return nil, false, err
		}
		if resp.Rcode != dns.RcodeSuccess {
			continue
		}
		if qtype == dns.TypeA {
			aNoError = true
		}
		for _, rr := range resp.Answer {
			switch record := rr.(type) {
			case *dns.A:
				out[record.A.String()] = true
			case *dns.AAAA:
				out[record.AAAA.String()] = true
			}
		}
	}
	return out, aNoError, nil
}

// labelAlphabet is the DNS-legal character set a probe label is drawn from.
// Hyphens are left out so a label can never be rejected for starting or ending
// with one.
const labelAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

// randomLabel builds a label no one has registered.
//
// It uses crypto/rand rather than math/rand so that two yatt processes started
// in the same instant cannot probe with the same label and cache each other's
// coincidence.
func randomLabel() (string, error) {
	var b strings.Builder
	b.Grow(labelLength)
	alphabetSize := big.NewInt(int64(len(labelAlphabet)))
	for i := 0; i < labelLength; i++ {
		n, err := rand.Int(rand.Reader, alphabetSize)
		if err != nil {
			return "", err
		}
		b.WriteByte(labelAlphabet[n.Int64()])
	}
	return b.String(), nil
}

// normalizeZone puts a zone in the form the cache is keyed by, so "COM." and
// "com" share one probe.
func normalizeZone(zone string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(zone)), ".")
}
