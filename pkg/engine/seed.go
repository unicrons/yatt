package engine

import (
	"fmt"
	"strings"

	"golang.org/x/net/publicsuffix"
)

// Seed is a parsed input domain, split into the parts the permutation
// techniques and the resolver each need.
//
// The split is public-suffix aware rather than a naive last-dot split. This is
// load-bearing twice over: techniques mutate only the SLD label, and the
// registered/unregistered NS query must target the registrable domain. A naive
// split of "example.co.uk" would yield the suffix "uk" and query "co.uk", which
// always answers NOERROR and would mark every candidate registered.
type Seed struct {
	// Subdomain is everything left of the registrable domain, without the
	// trailing dot. Empty when the input is already a registrable domain.
	Subdomain string
	// SLD is the label immediately left of the public suffix — the only part
	// permutation techniques are allowed to mutate.
	SLD string
	// Suffix is the effective top-level domain (public suffix), e.g. "com" or
	// "co.uk".
	Suffix string
}

// ParseSeed splits domain into its subdomain, SLD and public suffix.
//
// The public suffix list comes from golang.org/x/net/publicsuffix, which embeds
// a snapshot refreshed on each module release. If snapshot staleness ever
// matters, this function is the only seam a runtime-refreshable list would need
// to replace.
func ParseSeed(domain string) (Seed, error) {
	name := normalizeDomain(domain)
	if name == "" {
		return Seed{}, fmt.Errorf("empty domain")
	}
	if strings.ContainsAny(name, "/:\\ \t") {
		return Seed{}, fmt.Errorf("invalid domain %q: expected a bare domain name, not a URL", domain)
	}
	if !strings.Contains(name, ".") {
		return Seed{}, fmt.Errorf("invalid domain %q: missing a top-level domain", domain)
	}

	registrable, err := publicsuffix.EffectiveTLDPlusOne(name)
	if err != nil {
		return Seed{}, fmt.Errorf("cannot determine the registrable domain of %q: %w", domain, err)
	}
	suffix, _ := publicsuffix.PublicSuffix(name)

	sld := strings.TrimSuffix(registrable, "."+suffix)
	if sld == "" || sld == registrable {
		return Seed{}, fmt.Errorf("cannot determine the second-level label of %q", domain)
	}

	subdomain := strings.TrimSuffix(name, registrable)
	subdomain = strings.TrimSuffix(subdomain, ".")

	return Seed{Subdomain: subdomain, SLD: sld, Suffix: suffix}, nil
}

// Registrable returns the eTLD+1 — the name an NS query targets to decide
// whether a candidate is registered.
func (s Seed) Registrable() string {
	return s.SLD + "." + s.Suffix
}

// String returns the full domain the seed was parsed from, normalized.
func (s Seed) String() string {
	if s.Subdomain == "" {
		return s.Registrable()
	}
	return s.Subdomain + "." + s.Registrable()
}

// normalizeDomain lowercases the name and strips surrounding whitespace and the
// root dot, so "Example.COM." and "example.com" parse identically.
func normalizeDomain(domain string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
}
