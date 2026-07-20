package engine_test

import (
	"reflect"
	"testing"

	"github.com/andoniaf/yatt/pkg/engine"
)

func TestTLDPermuteSuffixesExcludesTheOriginal(t *testing.T) {
	got := engine.TLD{TLDs: []string{"net", "com", "org"}}.PermuteSuffixes("com")
	want := []string{"net", "org"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("PermuteSuffixes(\"com\") = %v, want %v", got, want)
	}
}

func TestTLDPermuteSuffixesDedupesAndNormalizes(t *testing.T) {
	got := engine.TLD{TLDs: []string{"NET", ".net", " net ", "org"}}.PermuteSuffixes("com")
	want := []string{"net", "org"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("PermuteSuffixes(\"com\") = %v, want %v", got, want)
	}
}

func TestTLDPermuteSuffixesPreservesOrder(t *testing.T) {
	got := engine.TLD{TLDs: []string{"xyz", "net", "io"}}.PermuteSuffixes("com")
	want := []string{"xyz", "net", "io"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("PermuteSuffixes(\"com\") = %v, want %v", got, want)
	}
}

func TestTLDZeroValueUsesTheCommonProfile(t *testing.T) {
	got := engine.TLD{}.PermuteSuffixes("com")
	if len(got) == 0 {
		t.Fatal("zero-value TLD{} produced nothing, want the default common profile")
	}
	for _, suffix := range got {
		if suffix == "com" {
			t.Error("default profile includes the seed's own suffix")
		}
	}
}

func TestTLDPermuteIsUnusedButSatisfiesTechnique(t *testing.T) {
	if got := (engine.TLD{}).Permute("example"); got != nil {
		t.Errorf("Permute(\"example\") = %v, want nil — TLD varies the suffix via PermuteSuffixes", got)
	}
}

func TestTLDName(t *testing.T) {
	if got := (engine.TLD{}).Name(); got != "tld" {
		t.Errorf("Name() = %q, want %q", got, "tld")
	}
}

func TestTLDBuildsCandidatesThroughPermute(t *testing.T) {
	seed, err := engine.ParseSeed("example.com")
	if err != nil {
		t.Fatalf("ParseSeed: %v", err)
	}

	got := engine.Permute(seed, []engine.Technique{engine.TLD{TLDs: []string{"net", "com"}}})
	if len(got) != 1 {
		t.Fatalf("got %d candidates, want 1", len(got))
	}
	c := got[0]
	if c.SLD != "example" {
		t.Errorf("SLD = %q, want %q — TLD swap keeps the seed's own label", c.SLD, "example")
	}
	if c.Suffix != "net" {
		t.Errorf("Suffix = %q, want %q", c.Suffix, "net")
	}
	if c.Domain != "example.net" {
		t.Errorf("Domain = %q, want %q", c.Domain, "example.net")
	}
	if c.Technique != "tld" {
		t.Errorf("Technique = %q, want %q", c.Technique, "tld")
	}
}
