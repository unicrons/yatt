package config_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/andoniaf/yatt/internal/config"
)

func writeConfig(t *testing.T, contents string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "yatt.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

// "profile alone": naming a builtin profile with no config file and no
// overrides returns it exactly as declared.
func TestResolveReturnsTheBuiltinProfileUnchanged(t *testing.T) {
	cfg := config.New()

	got, err := cfg.Resolve("quick")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !reflect.DeepEqual(got, config.Builtins["quick"]) {
		t.Errorf("Resolve(%q) = %+v, want the builtin unchanged: %+v", "quick", got, config.Builtins["quick"])
	}
}

func TestResolveEmptyNameIsTheZeroProfileNotAnError(t *testing.T) {
	cfg := config.New()

	got, err := cfg.Resolve("")
	if err != nil {
		t.Fatalf("Resolve(\"\"): %v", err)
	}
	if !reflect.DeepEqual(got, config.Profile{}) {
		t.Errorf("Resolve(\"\") = %+v, want the zero Profile", got)
	}
}

func TestResolveRejectsAnUnknownProfile(t *testing.T) {
	cfg := config.New()

	_, err := cfg.Resolve("bogus")
	if err == nil {
		t.Fatal("Resolve(\"bogus\") succeeded, want an error")
	}
	for _, want := range []string{"bogus", "quick", "full"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to mention %q", err, want)
		}
	}
}

// "config-file profile beating builtin": a same-named profile in the config
// file overrides the builtin's fields, without needing to restate every
// field the config file leaves alone.
func TestResolveConfigFileProfileOverridesTheBuiltinFieldByField(t *testing.T) {
	path := writeConfig(t, "profiles:\n  quick:\n    tld_profile: full\n")

	cfg := config.New()
	if err := cfg.Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}

	got, err := cfg.Resolve("quick")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.TLDProfile != "full" {
		t.Errorf("TLDProfile = %q, want the config file's %q to win", got.TLDProfile, "full")
	}
	// Everything the config file did not mention is still the builtin's, not
	// blanked by the config-file profile only naming one field.
	builtin := config.Builtins["quick"]
	if !reflect.DeepEqual(got.Techniques, builtin.Techniques) {
		t.Errorf("Techniques = %v, want the builtin's %v carried over", got.Techniques, builtin.Techniques)
	}
	if got.Concurrency != builtin.Concurrency {
		t.Errorf("Concurrency = %d, want the builtin's %d carried over", got.Concurrency, builtin.Concurrency)
	}
	if got.Timeout != builtin.Timeout {
		t.Errorf("Timeout = %v, want the builtin's %v carried over", got.Timeout, builtin.Timeout)
	}
}

// A config file may also define a profile with no builtin of the same name.
func TestResolveConfigFileDefinesANewProfile(t *testing.T) {
	path := writeConfig(t, "profiles:\n  custom:\n    techniques: [omission, tld]\n    limit: 25\n")

	cfg := config.New()
	if err := cfg.Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}

	got, err := cfg.Resolve("custom")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if want := []string{"omission", "tld"}; !reflect.DeepEqual(got.Techniques, want) {
		t.Errorf("Techniques = %v, want %v", got.Techniques, want)
	}
	if got.Limit != 25 {
		t.Errorf("Limit = %d, want 25", got.Limit)
	}
	// Nothing else was named, so it stays the zero value rather than
	// inheriting from an unrelated builtin.
	if got.Concurrency != 0 {
		t.Errorf("Concurrency = %d, want 0 (unset)", got.Concurrency)
	}
}

func TestNamesListsBuiltinsAndConfigFileProfiles(t *testing.T) {
	path := writeConfig(t, "profiles:\n  custom:\n    limit: 1\n")

	cfg := config.New()
	if err := cfg.Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}

	names := cfg.Names()
	for _, want := range []string{"quick", "full", "custom"} {
		if !containsString(names, want) {
			t.Errorf("Names() = %v, want it to contain %q", names, want)
		}
	}
}

// "profile + override flag": a field the caller reports as an explicitly
// changed flag is cleared, so applying the overlaid profile can never
// clobber it.
func TestOverlayClearsOnlyTheChangedFields(t *testing.T) {
	profile := config.Builtins["full"]

	changed := map[string]bool{"concurrency": true, "limit": true}
	got := profile.Overlay(func(field string) bool { return changed[field] })

	if got.Concurrency != 0 {
		t.Errorf("Concurrency = %d, want 0 after being reported changed", got.Concurrency)
	}
	if got.Limit != 0 {
		t.Errorf("Limit = %d, want 0 after being reported changed", got.Limit)
	}
	// Every other field survives untouched.
	if !reflect.DeepEqual(got.Techniques, profile.Techniques) {
		t.Errorf("Techniques = %v, want the profile's own %v", got.Techniques, profile.Techniques)
	}
	if got.TLDProfile != profile.TLDProfile {
		t.Errorf("TLDProfile = %q, want the profile's own %q", got.TLDProfile, profile.TLDProfile)
	}
}

// "unset flag not clobbering": a field the profile itself never set (its
// zero value) stays zero through Overlay regardless of what changed reports,
// so a caller that only applies non-zero fields can never blank a flag's own
// default or an earlier override with it.
func TestOverlayNeverInventsAValueForAFieldTheProfileLeftUnset(t *testing.T) {
	// "full" deliberately leaves Timeout and Limit unset.
	profile := config.Builtins["full"]
	if profile.Timeout != 0 || profile.Limit != 0 {
		t.Fatalf("test assumption broken: %+v", profile)
	}

	got := profile.Overlay(func(string) bool { return false })
	if got.Timeout != 0 {
		t.Errorf("Timeout = %v, want it to stay unset", got.Timeout)
	}
	if got.Limit != 0 {
		t.Errorf("Limit = %d, want it to stay unset", got.Limit)
	}
}

func TestProfileNameResolvesFromTheConfigFileWhenNoFlagOrEnvIsSet(t *testing.T) {
	path := writeConfig(t, "profile: quick\n")

	cfg := config.New()
	if err := cfg.Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.ProfileName(); got != "quick" {
		t.Errorf("ProfileName() = %q, want %q", got, "quick")
	}
}

func TestProfileNameEnvironmentBeatsTheConfigFile(t *testing.T) {
	path := writeConfig(t, "profile: quick\n")

	cfg := config.New()
	if err := cfg.Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}

	t.Setenv("YATT_PROFILE", "full")
	if got := cfg.ProfileName(); got != "full" {
		t.Errorf("ProfileName() = %q, want the environment's %q to win", got, "full")
	}
}

func TestLoadEmptyPathIsNotAnError(t *testing.T) {
	cfg := config.New()
	if err := cfg.Load(""); err != nil {
		t.Errorf("Load(\"\"): %v, want no error", err)
	}
}

func TestLoadRejectsAMissingFile(t *testing.T) {
	cfg := config.New()
	if err := cfg.Load("/no/such/file.yaml"); err == nil {
		t.Fatal("Load of a missing file succeeded, want an error")
	}
}

func containsString(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
