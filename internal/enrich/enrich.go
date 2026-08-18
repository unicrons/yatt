// Package enrich builds ready-to-open lookup links for a finding.
//
// By default it only formats URLs — no HTTP call leaves this package.
// Actually fetching AbuseIPDB or Shodan data would mean API keys, free-tier
// rate limits, and traffic that itself looks like scanning; a link an analyst
// opens by hand carries none of that.
//
// The one opt-in exception is Client, in client.go: when a caller explicitly
// asks for AbuseIPDB's Confidence of Abuse score, it makes the live API call
// that scores require. The pure link-building above never does this on its
// own — a Client must be constructed and used by the caller.
package enrich

import (
	"net/url"

	"github.com/unicrons/yatt/internal/scan"
)

// Enrichment is one ready-to-open lookup link.
type Enrichment struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	// Address is the IP the entry is about, set only for the by-address links
	// AddressLinks builds. It lets a caller find "the AbuseIPDB entry for IP
	// X" without positional index arithmetic into the slice ForFinding
	// returns.
	Address string `json:"address,omitempty"`
	// Score is AbuseIPDB's Confidence of Abuse percentage for Address,
	// populated only when a caller opted into the live lookup via Client.
	Score *int `json:"abuse_confidence_score,omitempty"`
}

// DomainLinks returns the by-domain lookups for domain: AbuseIPDB's WHOIS
// tool and Shodan's domain page. These exist even for a candidate DNS never
// resolved, since both services index hostnames independently of any
// address.
func DomainLinks(domain string) []Enrichment {
	return []Enrichment{
		{Name: "AbuseIPDB", URL: "https://www.abuseipdb.com/whois/" + url.PathEscape(domain)},
		{Name: "Shodan", URL: "https://www.shodan.io/domain/" + url.PathEscape(domain)},
	}
}

// AddressLinks returns the by-IP lookups for a single resolved address:
// AbuseIPDB's abuse-report check and Shodan's host page.
func AddressLinks(ip string) []Enrichment {
	return []Enrichment{
		{Name: "AbuseIPDB", URL: "https://www.abuseipdb.com/check/" + url.PathEscape(ip), Address: ip},
		{Name: "Shodan", URL: "https://www.shodan.io/host/" + url.PathEscape(ip), Address: ip},
	}
}

// ForFinding returns every enrichment link for a finding: the by-domain
// lookups, plus a by-IP pair for each resolved address. A finding with no
// resolved addresses still gets its by-domain links.
func ForFinding(f scan.Finding) []Enrichment {
	links := DomainLinks(f.Candidate)
	for _, addr := range f.Addresses {
		links = append(links, AddressLinks(addr)...)
	}
	return links
}
