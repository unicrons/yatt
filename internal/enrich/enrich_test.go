package enrich_test

import (
	"strings"
	"testing"

	"github.com/unicrons/yatt/internal/enrich"
	"github.com/unicrons/yatt/internal/scan"
)

func TestDomainLinksFormatsBothServices(t *testing.T) {
	links := enrich.DomainLinks("xample.com")
	if len(links) != 2 {
		t.Fatalf("got %d links, want 2", len(links))
	}
	want := map[string]string{
		"AbuseIPDB": "https://www.abuseipdb.com/whois/xample.com",
		"Shodan":    "https://www.shodan.io/domain/xample.com",
	}
	for _, l := range links {
		if l.URL != want[l.Name] {
			t.Errorf("%s URL = %q, want %q", l.Name, l.URL, want[l.Name])
		}
	}
}

func TestAddressLinksFormatsBothServices(t *testing.T) {
	links := enrich.AddressLinks("192.0.2.1")
	if len(links) != 2 {
		t.Fatalf("got %d links, want 2", len(links))
	}
	want := map[string]string{
		"AbuseIPDB": "https://www.abuseipdb.com/check/192.0.2.1",
		"Shodan":    "https://www.shodan.io/host/192.0.2.1",
	}
	for _, l := range links {
		if l.URL != want[l.Name] {
			t.Errorf("%s URL = %q, want %q", l.Name, l.URL, want[l.Name])
		}
		// Address lets a caller find "the AbuseIPDB entry for IP X" without
		// positional index arithmetic into the slice.
		if l.Address != "192.0.2.1" {
			t.Errorf("%s Address = %q, want %q", l.Name, l.Address, "192.0.2.1")
		}
	}
}

// DomainLinks has no address to report: it exists even for a candidate DNS
// never resolved.
func TestDomainLinksCarryNoAddress(t *testing.T) {
	for _, l := range enrich.DomainLinks("xample.com") {
		if l.Address != "" {
			t.Errorf("%s Address = %q, want empty for a by-domain link", l.Name, l.Address)
		}
	}
}

// A finding with no resolved address still gets its by-domain links: nothing
// answering is not a reason to leave an analyst with nothing to open.
func TestForFindingWithNoResolvedAddressStillReturnsDomainLinks(t *testing.T) {
	f := scan.Finding{Candidate: "xample.com"}

	links := enrich.ForFinding(f)
	if len(links) != 2 {
		t.Fatalf("got %d links, want exactly the 2 by-domain links: %+v", len(links), links)
	}
	for _, l := range links {
		if !strings.Contains(l.URL, "xample.com") {
			t.Errorf("link %+v does not reference the candidate", l)
		}
	}
}

func TestForFindingAddsAPairOfLinksPerResolvedAddress(t *testing.T) {
	f := scan.Finding{
		Candidate: "xample.com",
		Addresses: []string{"192.0.2.1", "192.0.2.2"},
	}

	links := enrich.ForFinding(f)
	// 2 by-domain + 2 addresses * 2 services each.
	if len(links) != 6 {
		t.Fatalf("got %d links, want 6: %+v", len(links), links)
	}
	for _, addr := range f.Addresses {
		var found int
		for _, l := range links {
			if strings.Contains(l.URL, addr) {
				found++
			}
		}
		if found != 2 {
			t.Errorf("address %s appears in %d links, want 2 (AbuseIPDB + Shodan)", addr, found)
		}
	}
}

func TestLinksEscapeTheirInput(t *testing.T) {
	links := enrich.DomainLinks("xn--exmple-cua.com")
	for _, l := range links {
		if strings.Contains(l.URL, " ") {
			t.Errorf("link %+v is not escaped", l)
		}
	}
}
