package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/unicrons/yatt/pkg/engine"
)

func writeYattConfig(t *testing.T, contents string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "yatt.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

// "profile alone": --profile quick restricts the techniques run to quick's
// set, which excludes homoglyph and tld.
func TestScanProfileQuickRestrictsTheTechniqueSet(t *testing.T) {
	withTempStore(t)
	withFakeResolver(t, scriptedResolver{})

	findings := decodeFindings(t, mustRun(t,
		"scan", "example.com", "--output", "json", "--profile", "quick", "--show-unregistered"))

	seen := map[string]bool{}
	for _, f := range findings {
		if f.IsOriginal() {
			continue
		}
		seen[f.Technique] = true
	}
	for _, want := range []string{"omission", "transposition", "keyboard"} {
		if !seen[want] {
			t.Errorf("quick profile produced no %q candidates", want)
		}
	}
	for _, absent := range []string{"homoglyph", "tld"} {
		if seen[absent] {
			t.Errorf("quick profile unexpectedly produced %q candidates", absent)
		}
	}
}

// --profile full runs every technique, including the two quick leaves out.
func TestScanProfileFullRunsEveryTechnique(t *testing.T) {
	withTempStore(t)
	withFakeResolver(t, scriptedResolver{})

	findings := decodeFindings(t, mustRun(t,
		"scan", "example.com", "--output", "json", "--profile", "full", "--show-unregistered"))

	seen := map[string]bool{}
	for _, f := range findings {
		seen[f.Technique] = true
	}
	for _, name := range engine.Names() {
		if !seen[name] {
			t.Errorf("full profile produced no %q candidates", name)
		}
	}
}

// "profile + override flag": an explicit --technique beats the profile's own
// technique selection.
func TestScanProfilePlusOverrideFlagLetsTheFlagWin(t *testing.T) {
	withTempStore(t)
	withFakeResolver(t, scriptedResolver{})

	findings := decodeFindings(t, mustRun(t,
		"scan", "example.com", "--output", "json",
		"--profile", "quick", "--technique", "homoglyph", "--show-unregistered"))

	for _, f := range findings {
		if f.IsOriginal() {
			continue
		}
		if f.Technique != "homoglyph" {
			t.Errorf("finding %q has technique %q, want only homoglyph (the flag should have beaten the profile)",
				f.Candidate, f.Technique)
		}
	}
}

// "unset flag not clobbering": overriding one profile field (--tld-profile)
// must not blank the technique selection the profile also set.
func TestScanProfileOverrideOfOneFieldLeavesOthersIntact(t *testing.T) {
	withTempStore(t)
	withFakeResolver(t, scriptedResolver{})

	findings := decodeFindings(t, mustRun(t,
		"scan", "example.com", "--output", "json",
		"--profile", "quick", "--tld-profile", "full", "--show-unregistered"))

	seen := map[string]bool{}
	for _, f := range findings {
		if f.IsOriginal() {
			continue
		}
		seen[f.Technique] = true
	}
	// The technique set is still quick's, untouched by overriding --tld-profile.
	for _, want := range []string{"omission", "transposition", "keyboard"} {
		if !seen[want] {
			t.Errorf("technique set lost %q after overriding an unrelated field", want)
		}
	}
	if seen["homoglyph"] || seen["tld"] {
		t.Errorf("overriding --tld-profile pulled in techniques outside the quick profile: %v", seen)
	}
}

// "config-file profile beating builtin": a config file overriding quick's
// tld_profile changes what scan actually does.
func TestScanConfigFileProfileOverridesTheBuiltin(t *testing.T) {
	withTempStore(t)
	withFakeResolver(t, scriptedResolver{})

	path := writeYattConfig(t, "profiles:\n  quick:\n    techniques: [tld]\n")

	findings := decodeFindings(t, mustRun(t,
		"--config", path, "scan", "example.com", "--output", "json", "--profile", "quick", "--show-unregistered"))

	for _, f := range findings {
		if f.IsOriginal() {
			continue
		}
		if f.Technique != "tld" {
			t.Errorf("finding %q has technique %q, want only tld (the config-file profile should have won)",
				f.Candidate, f.Technique)
		}
	}
}

func TestScanRejectsAnUnknownProfile(t *testing.T) {
	withTempStore(t)
	withFakeResolver(t, scriptedResolver{})

	_, _, err := run(t, "scan", "example.com", "--profile", "bogus")
	if err == nil {
		t.Fatal("scan --profile bogus succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Errorf("error = %q, want it to name the bad profile", err)
	}
}

func TestScanRejectsAMissingConfigFile(t *testing.T) {
	withTempStore(t)
	withFakeResolver(t, scriptedResolver{})

	_, _, err := run(t, "--config", "/no/such/file.yaml", "scan", "example.com")
	if err == nil {
		t.Fatal("scan --config /no/such/file.yaml succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "--config") {
		t.Errorf("error = %q, want it to name the flag", err)
	}
}

// The recorded scan carries the profile name, so `yatt history` explains why
// two scans of one seed differ in size.
func TestScanRecordsTheProfileNameOnTheScan(t *testing.T) {
	withTempStore(t)
	withFakeResolver(t, scriptedResolver{})

	mustRun(t, "scan", "example.com", "--profile", "quick")

	stdout := mustRun(t, "history", "example.com", "--output", "json")
	if !strings.Contains(stdout, `"profile": "quick"`) {
		t.Errorf("history = %q, want the recorded scan to carry the profile name", stdout)
	}
}
