package cmd

import (
	"strings"
	"testing"
)

// registeredCandidates is the scripted set these tests scan against: two of the
// omission candidates are taken, the rest are not.
func withPartiallyRegistered(t *testing.T) {
	t.Helper()
	withFakeResolver(t, scriptedResolver{registered: map[string]bool{
		"xample.com": true,
		"exampl.com": true,
	}})
}

// TestScanHidesUnregisteredByDefault is the headline behaviour: an analyst
// running a bare scan sees only what somebody actually took.
func TestScanHidesUnregisteredByDefault(t *testing.T) {
	withTempStore(t)
	withPartiallyRegistered(t)

	findings := decodeFindings(t, mustRun(t,
		"scan", "example.com", "--output", "json", "--technique", "omission"))

	for _, f := range findings {
		if f.IsOriginal() {
			continue
		}
		if !f.Registered {
			t.Errorf("default scan reported unregistered candidate %q", f.Candidate)
		}
	}
	if got := candidateNames(findings); len(got) != 3 {
		t.Errorf("default scan reported %v, want the seed plus the 2 registered candidates", got)
	}
}

// TestScanShowUnregisteredAddsThemBack is the flag's whole job.
func TestScanShowUnregisteredAddsThemBack(t *testing.T) {
	withTempStore(t)
	withPartiallyRegistered(t)

	hidden := decodeFindings(t, mustRun(t,
		"scan", "example.com", "--output", "json", "--technique", "omission"))
	shown := decodeFindings(t, mustRun(t,
		"scan", "example.com", "--output", "json", "--technique", "omission", "--show-unregistered"))

	if len(shown) <= len(hidden) {
		t.Fatalf("--show-unregistered reported %d rows, want more than the default's %d",
			len(shown), len(hidden))
	}
	if got := candidateNames(shown); len(got) != seedCandidateCount+1 {
		t.Errorf("--show-unregistered reported %d rows, want every candidate plus the seed: %v",
			len(got), got)
	}
}

// TestScanLeadsWithRegisteredCandidates covers the ordering half: with
// everything shown, the rows worth acting on still come first.
func TestScanLeadsWithRegisteredCandidates(t *testing.T) {
	withTempStore(t)
	withPartiallyRegistered(t)

	findings := decodeFindings(t, mustRun(t,
		"scan", "example.com", "--output", "json", "--technique", "omission", "--show-unregistered"))

	if len(findings) == 0 || !findings[0].IsOriginal() {
		t.Fatalf("report = %v, want it to still lead with the seed", candidateNames(findings))
	}

	// Past the seed, every registered candidate must precede every
	// unregistered one.
	var seenUnregistered bool
	for _, f := range findings[1:] {
		if !f.Registered {
			seenUnregistered = true
			continue
		}
		if seenUnregistered {
			t.Errorf("registered candidate %q appears after an unregistered one: %v",
				f.Candidate, candidateNames(findings))
			break
		}
	}
}

// TestScanReportsCandidatesThatFailedToResolve guards the carve-out: a lookup
// that errored is not evidence the domain is free, so hiding it would quietly
// shorten a partially-failed scan.
func TestScanReportsCandidatesThatFailedToResolve(t *testing.T) {
	withTempStore(t)
	withFakeResolver(t, scriptedResolver{
		registered: map[string]bool{},
		fail:       map[string]bool{"xample.com": true},
	})

	findings := decodeFindings(t, mustRun(t,
		"scan", "example.com", "--output", "json", "--technique", "omission"))

	failed := findingFor(t, findings, "xample.com")
	if failed.Error == "" {
		t.Errorf("finding = %+v, want the resolution error recorded", failed)
	}
	if failed.Registered {
		t.Errorf("finding = %+v, want it reported as not registered", failed)
	}
}

// TestScanVerboseReportsWhatItHid keeps the default discoverable: a scan that
// silently drops most of its candidates must say so, and name the way back.
func TestScanVerboseReportsWhatItHid(t *testing.T) {
	withTempStore(t)
	withPartiallyRegistered(t)

	_, stderr, err := run(t, "scan", "example.com", "--technique", "omission", "--verbose")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	if !strings.Contains(stderr, "--show-unregistered") {
		t.Errorf("verbose output does not mention the flag that reveals them:\n%s", stderr)
	}
}
