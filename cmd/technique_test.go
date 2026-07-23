package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/unicrons/yatt/pkg/engine"
)

// TestScanDefaultsToEveryTechnique guards Phase 5's headline default-scope
// change: with no --technique flag, a scan runs every registered technique,
// not just omission.
func TestScanDefaultsToEveryTechnique(t *testing.T) {
	withTempStore(t)
	withFakeResolver(t, scriptedResolver{})

	// --show-unregistered because this asserts on which techniques ran, and the
	// scripted resolver registers nothing.
	findings := decodeFindings(t, mustRun(t, "scan", "example.com", "--output", "json", "--show-unregistered"))

	seen := make(map[string]bool)
	for _, f := range findings {
		seen[f.Technique] = true
	}
	for _, name := range engine.Names() {
		if !seen[name] {
			t.Errorf("default scan produced no %q candidates", name)
		}
	}
}

// TestScanTechniqueFlagRestrictsTheSet is the flip side: naming techniques
// explicitly must restrict the report to exactly those.
func TestScanTechniqueFlagRestrictsTheSet(t *testing.T) {
	withTempStore(t)
	withFakeResolver(t, scriptedResolver{})

	findings := decodeFindings(t, mustRun(t,
		"scan", "example.com", "--output", "json", "--technique", "omission,transposition"))

	for _, f := range findings {
		if f.IsOriginal() {
			continue
		}
		if f.Technique != "omission" && f.Technique != "transposition" {
			t.Errorf("finding %q has technique %q, want only omission/transposition", f.Candidate, f.Technique)
		}
	}
}

func TestScanRejectsAnUnknownTechnique(t *testing.T) {
	withTempStore(t)
	withFakeResolver(t, scriptedResolver{})

	_, _, err := run(t, "scan", "example.com", "--technique", "bogus")
	if err == nil {
		t.Fatal("scan --technique bogus succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Errorf("error = %q, want it to name the bad technique", err)
	}
}

// TestScanTLDProfileFullExpandsTheCandidateCount checks the --tld-profile
// axis end to end: "full" sweeps the whole IANA list, so it must produce
// visibly more tld candidates than the seed-aware "common" default.
func TestScanTLDProfileFullExpandsTheCandidateCount(t *testing.T) {
	withTempStore(t)
	withFakeResolver(t, scriptedResolver{})

	common := decodeFindings(t, mustRun(t,
		"scan", "example.com", "--output", "json", "--technique", "tld", "--tld-profile", "common",
		"--show-unregistered"))
	full := decodeFindings(t, mustRun(t,
		"scan", "example.com", "--output", "json", "--technique", "tld", "--tld-profile", "full",
		"--show-unregistered"))

	if len(full) <= len(common) {
		t.Errorf("full profile produced %d findings, want more than common's %d", len(full), len(common))
	}
}

func TestScanRejectsAnUnknownTLDProfile(t *testing.T) {
	withTempStore(t)
	withFakeResolver(t, scriptedResolver{})

	_, _, err := run(t, "scan", "example.com", "--technique", "tld", "--tld-profile", "bogus")
	if err == nil {
		t.Fatal("scan --tld-profile bogus succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Errorf("error = %q, want it to name the bad profile", err)
	}
}

// TestScanTLDFileOverridesTheProfile checks --tld-file end to end: a custom
// list is used instead of the named profile, in the order given.
func TestScanTLDFileOverridesTheProfile(t *testing.T) {
	withTempStore(t)
	withFakeResolver(t, scriptedResolver{})

	path := writeTLDFile(t, "net\nxyz\n")

	findings := decodeFindings(t, mustRun(t,
		"scan", "example.com", "--output", "json", "--technique", "tld", "--tld-file", path,
		"--show-unregistered"))

	var suffixes []string
	for _, f := range findings {
		if f.IsOriginal() {
			continue
		}
		suffixes = append(suffixes, strings.TrimPrefix(f.Candidate, "example."))
	}
	want := []string{"net", "xyz"}
	if len(suffixes) != len(want) {
		t.Fatalf("suffixes = %v, want %v", suffixes, want)
	}
	for i := range want {
		if suffixes[i] != want[i] {
			t.Errorf("suffixes[%d] = %q, want %q", i, suffixes[i], want[i])
		}
	}
}

// An explicitly given file that yields no TLDs is an error: scan.Run treats
// an empty list as "no custom list" and would silently sweep the
// --tld-profile the file was meant to replace.
func TestScanRejectsAnEmptyTLDFile(t *testing.T) {
	withTempStore(t)
	withFakeResolver(t, scriptedResolver{})

	path := writeTLDFile(t, "# every entry commented out\n\n")

	_, _, err := run(t, "scan", "example.com", "--technique", "tld", "--tld-file", path)
	if err == nil {
		t.Fatal("scan with a comment-only --tld-file succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "no TLDs") {
		t.Errorf("error = %q, want it to say the file held no TLDs", err)
	}
}

func TestScanRejectsAMissingTLDFile(t *testing.T) {
	withTempStore(t)
	withFakeResolver(t, scriptedResolver{})

	_, _, err := run(t, "scan", "example.com", "--technique", "tld", "--tld-file", "/no/such/file")
	if err == nil {
		t.Fatal("scan --tld-file /no/such/file succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "--tld-file") {
		t.Errorf("error = %q, want it to name the flag", err)
	}
}

// TestScanLimitBoundsTheCandidateCount checks --limit end to end: it caps
// the total, but the seed's own row survives regardless.
func TestScanLimitBoundsTheCandidateCount(t *testing.T) {
	withTempStore(t)
	withFakeResolver(t, scriptedResolver{})

	findings := decodeFindings(t, mustRun(t,
		"scan", "example.com", "--output", "json", "--technique", "omission", "--limit", "3",
		"--show-unregistered"))

	if len(findings) != 4 { // the seed's own row, plus 3 candidates
		t.Fatalf("got %d findings, want 4 (the seed plus 3 candidates)", len(findings))
	}
	if !findings[0].IsOriginal() {
		t.Errorf("first finding = %+v, want the seed's own row", findings[0])
	}
}

// writeTLDFile writes a temporary TLD list and returns its path.
func writeTLDFile(t *testing.T, contents string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "tlds.txt")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}
