package engine_test

import (
	"reflect"
	"testing"

	"github.com/andoniaf/yatt/pkg/engine"
)

func TestOmissionPermute(t *testing.T) {
	tests := []struct {
		name string
		sld  string
		want []string
	}{
		{
			name: "each position dropped in turn",
			sld:  "abcd",
			want: []string{"bcd", "acd", "abd", "abc"},
		},
		{
			name: "repeated characters collapse to one variant",
			sld:  "google",
			want: []string{"oogle", "gogle", "goole", "googe", "googl"},
		},
		{
			name: "two character label yields both single characters",
			sld:  "ab",
			want: []string{"b", "a"},
		},
		{
			name: "single character label yields nothing",
			sld:  "a",
			want: nil,
		},
		{
			name: "empty label yields nothing",
			sld:  "",
			want: nil,
		},
		{
			// A byte-wise implementation would split the two-byte "à" and emit
			// invalid UTF-8 here; the rune-wise one drops it whole.
			name: "multibyte runes are dropped whole, not by byte",
			sld:  "exàmple",
			want: []string{"xàmple", "eàmple", "exmple", "exàple", "exàmle", "exàmpe", "exàmpl"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := engine.Omission{}.Permute(tt.sld)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Permute(%q) = %v, want %v", tt.sld, got, tt.want)
			}
		})
	}
}

func TestOmissionIsDeterministic(t *testing.T) {
	technique := engine.Omission{}
	first := technique.Permute("example")
	for i := 0; i < 10; i++ {
		got := technique.Permute("example")
		if !reflect.DeepEqual(got, first) {
			t.Fatalf("run %d differed: %v != %v", i, got, first)
		}
	}
}

func TestPermuteBuildsCandidates(t *testing.T) {
	seed, err := engine.ParseSeed("example.co.uk")
	if err != nil {
		t.Fatalf("ParseSeed: %v", err)
	}

	got := engine.Permute(seed, []engine.Technique{engine.Omission{}})
	if len(got) != len("example") {
		t.Fatalf("got %d candidates, want %d", len(got), len("example"))
	}

	first := got[0]
	if first.SLD != "xample" {
		t.Errorf("SLD = %q, want %q", first.SLD, "xample")
	}
	// The suffix must survive intact: a naive split would have produced
	// "xample.uk" and lost the "co" label.
	if first.Registrable != "xample.co.uk" {
		t.Errorf("Registrable = %q, want %q", first.Registrable, "xample.co.uk")
	}
	if first.Domain != "xample.co.uk" {
		t.Errorf("Domain = %q, want %q", first.Domain, "xample.co.uk")
	}
	if first.Technique != "omission" {
		t.Errorf("Technique = %q, want %q", first.Technique, "omission")
	}
}

func TestPermuteReattachesSubdomain(t *testing.T) {
	seed, err := engine.ParseSeed("www.example.com")
	if err != nil {
		t.Fatalf("ParseSeed: %v", err)
	}

	got := engine.Permute(seed, []engine.Technique{engine.Omission{}})
	if len(got) == 0 {
		t.Fatal("no candidates")
	}
	if got[0].Domain != "www.xample.com" {
		t.Errorf("Domain = %q, want %q", got[0].Domain, "www.xample.com")
	}
	// The NS query still targets the registrable domain, not the subdomain.
	if got[0].Registrable != "xample.com" {
		t.Errorf("Registrable = %q, want %q", got[0].Registrable, "xample.com")
	}
}

func TestPermuteDedupesAcrossTechniques(t *testing.T) {
	seed, err := engine.ParseSeed("example.com")
	if err != nil {
		t.Fatalf("ParseSeed: %v", err)
	}

	// Two techniques producing overlapping output must yield one candidate per
	// domain, attributed to the first technique that produced it.
	got := engine.Permute(seed, []engine.Technique{engine.Omission{}, engine.Omission{}})

	seen := make(map[string]bool)
	for _, c := range got {
		if seen[c.Registrable] {
			t.Errorf("duplicate candidate %q", c.Registrable)
		}
		seen[c.Registrable] = true
		if c.Technique != "omission" {
			t.Errorf("Technique = %q, want %q", c.Technique, "omission")
		}
	}
}

func TestPermuteSkipsTheSeedItself(t *testing.T) {
	seed, err := engine.ParseSeed("aa.com")
	if err != nil {
		t.Fatalf("ParseSeed: %v", err)
	}

	// "aa" omits to "a" twice, which dedupes; nothing may equal the seed.
	for _, c := range engine.Permute(seed, []engine.Technique{engine.Omission{}}) {
		if c.Registrable == seed.Registrable() {
			t.Errorf("candidate %q equals the seed", c.Registrable)
		}
	}
}

func TestPermuteRejectsInvalidLabels(t *testing.T) {
	seed, err := engine.ParseSeed("-a-.com")
	if err != nil {
		t.Skipf("seed not parseable: %v", err)
	}

	for _, c := range engine.Permute(seed, []engine.Technique{engine.Omission{}}) {
		if !engine.ValidLabel(c.SLD) {
			t.Errorf("invalid label %q survived", c.SLD)
		}
	}
}

func TestValidLabel(t *testing.T) {
	tests := []struct {
		label string
		want  bool
	}{
		{"example", true},
		{"ex-ample", true},
		{"e", true},
		{"123", true},
		{"", false},
		{"-example", false},
		{"example-", false},
		{"exa.mple", false},
		{"exa mple", false},
		{"exa_mple", false},
	}

	for _, tt := range tests {
		if got := engine.ValidLabel(tt.label); got != tt.want {
			t.Errorf("ValidLabel(%q) = %v, want %v", tt.label, got, tt.want)
		}
	}
}
