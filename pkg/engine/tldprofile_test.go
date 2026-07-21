package engine_test

import (
	"strings"
	"testing"

	"github.com/andoniaf/yatt/pkg/engine"
	"github.com/andoniaf/yatt/pkg/engine/data"
)

func TestResolveTLDProfile(t *testing.T) {
	tests := []struct {
		name    string
		profile string
		want    string // "common" or "full", checked by size class below
	}{
		{name: "empty defaults to common", profile: "", want: "common"},
		{name: "explicit common", profile: "common", want: "common"},
		{name: "full", profile: "full", want: "full"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := engine.ResolveTLDProfile(tt.profile, "com")
			if err != nil {
				t.Fatalf("ResolveTLDProfile: %v", err)
			}
			if len(got) == 0 {
				t.Fatal("no TLDs resolved")
			}
			if tt.want == "full" && len(got) != len(data.IANATLDs) {
				t.Errorf("full profile returned %d TLDs, want the full IANA list (%d)", len(got), len(data.IANATLDs))
			}
			if tt.want == "common" && len(got) >= len(data.IANATLDs) {
				t.Errorf("common profile returned %d TLDs, want fewer than the full IANA list (%d)", len(got), len(data.IANATLDs))
			}
		})
	}
}

func TestResolveTLDProfileRejectsUnknownNames(t *testing.T) {
	_, err := engine.ResolveTLDProfile("bogus", "com")
	if err == nil {
		t.Fatal("ResolveTLDProfile(\"bogus\", ...) succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Errorf("error = %q, want it to name the bad profile", err)
	}
}

func TestCommonTLDsIsSeedAware(t *testing.T) {
	// .com's registrants are known to be confused by .co, .cm, .om (single
	// character edits of "com") and .co.uk (curated, since edit distance
	// cannot reach a multi-label suffix).
	got := engine.CommonTLDs("com")
	for _, want := range []string{"co", "cm", "om", "co.uk"} {
		found := false
		for _, tld := range got {
			if tld == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("CommonTLDs(\"com\") = %v, missing near-miss %q", got, want)
		}
	}
}

func TestCommonTLDsLeadsWithNearMisses(t *testing.T) {
	// The seed's own near-misses are the highest-value candidates (closest
	// to a real mistake), so they must lead the curated common/abused lists
	// rather than being buried in them.
	got := engine.CommonTLDs("com")
	nearMisses := engine.NearMisses("com")
	if len(nearMisses) == 0 {
		t.Fatal("no near-misses for \"com\" to compare against")
	}
	if len(got) < len(nearMisses) {
		t.Fatalf("CommonTLDs returned fewer entries than NearMisses alone")
	}
	for i, want := range nearMisses {
		if got[i] != want {
			t.Errorf("CommonTLDs(\"com\")[%d] = %q, want near-miss %q to lead", i, got[i], want)
			break
		}
	}
}

func TestCommonTLDsDedupesAcrossLists(t *testing.T) {
	got := engine.CommonTLDs("com")
	seen := make(map[string]bool, len(got))
	for _, tld := range got {
		if seen[tld] {
			t.Errorf("duplicate TLD %q", tld)
		}
		seen[tld] = true
	}
}

func TestNearMissesOnlyReturnsRealTLDs(t *testing.T) {
	for _, tld := range engine.NearMisses("com") {
		if !strings.Contains(tld, ".") && !data.IANATLDSet[tld] {
			t.Errorf("NearMisses(\"com\") returned %q, which is not a real IANA TLD", tld)
		}
	}
}

func TestNearMissesOfAMultiLabelSuffixDoesNotPanic(t *testing.T) {
	// Character-edit distance does not apply to a multi-label suffix; this
	// must return cleanly (possibly empty) rather than panicking or
	// producing nonsense like "co.k".
	got := engine.NearMisses("co.uk")
	for _, tld := range got {
		if tld == "co.uk" {
			t.Error("NearMisses(\"co.uk\") returned the seed's own suffix")
		}
	}
}

func TestParseTLDList(t *testing.T) {
	input := strings.NewReader("com\n.net\n  \n# a comment\nORG\n")
	got, err := engine.ParseTLDList(input)
	if err != nil {
		t.Fatalf("ParseTLDList: %v", err)
	}
	want := []string{"com", "net", "org"}
	if len(got) != len(want) {
		t.Fatalf("ParseTLDList = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ParseTLDList[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestResolveTLDsOnlyRewritesTheTLDTechnique(t *testing.T) {
	techniques := []engine.Technique{engine.Omission{}, engine.TLD{}}
	resolved, err := engine.ResolveTLDs(techniques, "com", "", []string{"net", "org"})
	if err != nil {
		t.Fatalf("ResolveTLDs: %v", err)
	}
	if len(resolved) != 2 {
		t.Fatalf("got %d techniques, want 2", len(resolved))
	}
	if _, ok := resolved[0].(engine.Omission); !ok {
		t.Errorf("resolved[0] = %T, want engine.Omission untouched", resolved[0])
	}
	tld, ok := resolved[1].(engine.TLD)
	if !ok {
		t.Fatalf("resolved[1] = %T, want engine.TLD", resolved[1])
	}
	want := []string{"net", "org"}
	if len(tld.TLDs) != len(want) || tld.TLDs[0] != want[0] || tld.TLDs[1] != want[1] {
		t.Errorf("resolved TLD.TLDs = %v, want %v", tld.TLDs, want)
	}
}

func TestResolveTLDsIsNoOpWithoutTheTLDTechnique(t *testing.T) {
	techniques := []engine.Technique{engine.Omission{}}
	resolved, err := engine.ResolveTLDs(techniques, "com", "bogus-profile-name", nil)
	if err != nil {
		t.Fatalf("ResolveTLDs: %v, want no work done (and so no error) when TLD swap was not selected", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("got %d techniques, want 1", len(resolved))
	}
}

func TestResolveTLDsPropagatesAnUnknownProfile(t *testing.T) {
	techniques := []engine.Technique{engine.TLD{}}
	_, err := engine.ResolveTLDs(techniques, "com", "bogus-profile-name", nil)
	if err == nil {
		t.Fatal("ResolveTLDs succeeded with an unknown profile, want an error")
	}
}
