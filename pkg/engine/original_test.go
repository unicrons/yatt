package engine_test

import (
	"reflect"
	"testing"

	"github.com/unicrons/yatt/pkg/engine"
)

// mustParseSeed parses a seed that the test expects to be valid.
func mustParseSeed(t *testing.T, domain string) engine.Seed {
	t.Helper()

	seed, err := engine.ParseSeed(domain)
	if err != nil {
		t.Fatalf("ParseSeed(%q): %v", domain, err)
	}
	return seed
}

// domains reduces a candidate list to the names, for readable comparisons.
func domains(candidates []engine.Candidate) []string {
	out := make([]string, 0, len(candidates))
	for _, c := range candidates {
		out = append(out, c.Domain)
	}
	return out
}

func TestOriginal(t *testing.T) {
	tests := []struct {
		name            string
		seed            string
		wantDomain      string
		wantRegistrable string
		wantSLD         string
	}{
		{
			name:            "registrable seed",
			seed:            "example.com",
			wantDomain:      "example.com",
			wantRegistrable: "example.com",
			wantSLD:         "example",
		},
		{
			// The subdomain rides on Domain but must never reach Registrable:
			// the NS query that decides "registered" targets the eTLD+1.
			name:            "seed with a subdomain",
			seed:            "www.example.com",
			wantDomain:      "www.example.com",
			wantRegistrable: "example.com",
			wantSLD:         "example",
		},
		{
			name:            "seed with a deep subdomain and a multi-label suffix",
			seed:            "a.b.example.co.uk",
			wantDomain:      "a.b.example.co.uk",
			wantRegistrable: "example.co.uk",
			wantSLD:         "example",
		},
		{
			name:            "the seed is normalized like any other input",
			seed:            "WWW.Example.COM.",
			wantDomain:      "www.example.com",
			wantRegistrable: "example.com",
			wantSLD:         "example",
		},
		{
			// The resolver sends names verbatim, so the seed's own row must
			// carry the punycode wire form: raw UTF-8 on the wire answers
			// NXDOMAIN, which would record the user's own domain as
			// unregistered and invert the baseline of every scan.
			name:            "an IDN seed is converted to its wire form",
			seed:            "münchen.de",
			wantDomain:      "xn--mnchen-3ya.de",
			wantRegistrable: "xn--mnchen-3ya.de",
			wantSLD:         "xn--mnchen-3ya",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := engine.Original(mustParseSeed(t, tt.seed))

			if got.Domain != tt.wantDomain {
				t.Errorf("Domain = %q, want %q", got.Domain, tt.wantDomain)
			}
			if got.Registrable != tt.wantRegistrable {
				t.Errorf("Registrable = %q, want %q", got.Registrable, tt.wantRegistrable)
			}
			if got.SLD != tt.wantSLD {
				t.Errorf("SLD = %q, want %q", got.SLD, tt.wantSLD)
			}
			if got.Technique != engine.TechniqueOriginal {
				t.Errorf("Technique = %q, want %q", got.Technique, engine.TechniqueOriginal)
			}
		})
	}
}

// The seed's row is labelled, not generated: nothing may register a technique
// under the name that marks it, or a candidate could masquerade as the baseline.
func TestTechniqueOriginalIsNotARegisteredTechnique(t *testing.T) {
	if _, err := engine.Lookup(engine.TechniqueOriginal); err == nil {
		t.Errorf("Lookup(%q) succeeded, want the seed's label to be unregistered", engine.TechniqueOriginal)
	}
	for _, name := range engine.Names() {
		if name == engine.TechniqueOriginal {
			t.Errorf("technique %q is registered, want the seed's label reserved", name)
		}
	}
}

func TestWithOriginal(t *testing.T) {
	tests := []struct {
		name       string
		seed       string
		candidates []engine.Candidate
		want       []string
	}{
		{
			name:       "an empty candidate set still yields the seed row",
			seed:       "example.com",
			candidates: nil,
			want:       []string{"example.com"},
		},
		{
			name:       "an allocated but empty candidate set still yields the seed row",
			seed:       "example.com",
			candidates: []engine.Candidate{},
			want:       []string{"example.com"},
		},
		{
			name: "the seed leads and the candidates keep their order",
			seed: "example.com",
			candidates: []engine.Candidate{
				{Domain: "xample.com", Registrable: "xample.com", SLD: "xample", Technique: "omission"},
				{Domain: "eample.com", Registrable: "eample.com", SLD: "eample", Technique: "omission"},
			},
			want: []string{"example.com", "xample.com", "eample.com"},
		},
		{
			// Belt and braces against a technique that mutates the suffix rather
			// than the label: a TLD swap can in principle land back on the seed.
			name: "a candidate equal to the seed is dropped rather than emitted twice",
			seed: "example.com",
			candidates: []engine.Candidate{
				{Domain: "xample.com", Registrable: "xample.com", SLD: "xample", Technique: "omission"},
				{Domain: "example.com", Registrable: "example.com", SLD: "example", Technique: "tld"},
				{Domain: "eample.com", Registrable: "eample.com", SLD: "eample", Technique: "omission"},
			},
			want: []string{"example.com", "xample.com", "eample.com"},
		},
		{
			name: "the duplicate check reads the full name, not the registrable domain",
			seed: "www.example.com",
			candidates: []engine.Candidate{
				// Same registrable domain, different name: this is a real
				// candidate and must survive.
				{Domain: "wwww.example.com", Registrable: "example.com", SLD: "example", Technique: "keyboard"},
				{Domain: "www.example.com", Registrable: "example.com", SLD: "example", Technique: "tld"},
			},
			want: []string{"www.example.com", "wwww.example.com"},
		},
		{
			name: "several candidates equal to the seed are all dropped",
			seed: "example.com",
			candidates: []engine.Candidate{
				{Domain: "example.com", Registrable: "example.com", SLD: "example", Technique: "tld"},
				{Domain: "example.com", Registrable: "example.com", SLD: "example", Technique: "homoglyph"},
			},
			want: []string{"example.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := engine.WithOriginal(mustParseSeed(t, tt.seed), tt.candidates)

			if names := domains(got); !reflect.DeepEqual(names, tt.want) {
				t.Fatalf("WithOriginal() = %v, want %v", names, tt.want)
			}
			if got[0].Technique != engine.TechniqueOriginal {
				t.Errorf("first row technique = %q, want %q", got[0].Technique, engine.TechniqueOriginal)
			}
			for _, c := range got[1:] {
				if c.Technique == engine.TechniqueOriginal {
					t.Errorf("candidate %q is labelled %q, want only the leading row to be",
						c.Domain, engine.TechniqueOriginal)
				}
			}
		})
	}
}

func TestWithOriginalDoesNotMutateItsInput(t *testing.T) {
	candidates := []engine.Candidate{
		{Domain: "xample.com", Registrable: "xample.com", SLD: "xample", Technique: "omission"},
		{Domain: "eample.com", Registrable: "eample.com", SLD: "eample", Technique: "omission"},
	}
	before := append([]engine.Candidate(nil), candidates...)

	engine.WithOriginal(mustParseSeed(t, "example.com"), candidates)

	if !reflect.DeepEqual(candidates, before) {
		t.Errorf("WithOriginal mutated its input: %v, want %v", candidates, before)
	}
}

// The whole point of the row: the seed travels through the same pipeline as the
// candidates, so it is resolved, stored and diffed on one code path.
func TestWithOriginalOverRealPermutation(t *testing.T) {
	seed := mustParseSeed(t, "www.example.co.uk")
	candidates := engine.Permute(seed, []engine.Technique{engine.Omission{}})
	if len(candidates) == 0 {
		t.Fatal("no candidates to prepend the seed to")
	}

	got := engine.WithOriginal(seed, candidates)

	if len(got) != len(candidates)+1 {
		t.Fatalf("got %d rows, want %d candidates plus the seed", len(got), len(candidates))
	}
	if got[0].Domain != "www.example.co.uk" || got[0].Registrable != "example.co.uk" {
		t.Errorf("first row = %+v, want the seed www.example.co.uk / example.co.uk", got[0])
	}
	if !reflect.DeepEqual(domains(got[1:]), domains(candidates)) {
		t.Errorf("candidates = %v, want them unchanged below the seed: %v",
			domains(got[1:]), domains(candidates))
	}

	seen := make(map[string]bool, len(got))
	for _, c := range got {
		if seen[c.Domain] {
			t.Errorf("duplicate row %q", c.Domain)
		}
		seen[c.Domain] = true
	}
}
