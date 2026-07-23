// Package enrich builds ready-to-open lookup links for a finding.
//
// It only formats URLs — no HTTP call ever leaves this package. Actually
// fetching AbuseIPDB or Shodan data would mean API keys, free-tier rate
// limits, and traffic that itself looks like scanning; a link an analyst
// opens by hand carries none of that.
package enrich

import (
	"net/url"

	"github.com/unicrons/yatt/internal/scan"
)

// Enrichment is one ready-to-open lookup link.
type Enrichment struct {
	Name string `json:"name"`
	URL  string `json:"url"`
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
		{Name: "AbuseIPDB", URL: "https://www.abuseipdb.com/check/" + url.PathEscape(ip)},
		{Name: "Shodan", URL: "https://www.shodan.io/host/" + url.PathEscape(ip)},
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
