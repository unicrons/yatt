package engine_test

import (
	"testing"

	"github.com/unicrons/yatt/pkg/engine"
)

func TestParseSeed(t *testing.T) {
	tests := []struct {
		name            string
		input           string
		wantSubdomain   string
		wantSLD         string
		wantSuffix      string
		wantRegistrable string
	}{
		{
			name:            "simple domain",
			input:           "example.com",
			wantSLD:         "example",
			wantSuffix:      "com",
			wantRegistrable: "example.com",
		},
		{
			// The case the naive last-dot split gets wrong: it would call the
			// suffix "uk" and query "co.uk", which always answers NOERROR and
			// would mark every candidate registered.
			name:            "multi-label public suffix",
			input:           "example.co.uk",
			wantSLD:         "example",
			wantSuffix:      "co.uk",
			wantRegistrable: "example.co.uk",
		},
		{
			name:            "subdomain is separated from the registrable domain",
			input:           "www.example.com",
			wantSubdomain:   "www",
			wantSLD:         "example",
			wantSuffix:      "com",
			wantRegistrable: "example.com",
		},
		{
			name:            "deep subdomain",
			input:           "a.b.example.co.uk",
			wantSubdomain:   "a.b",
			wantSLD:         "example",
			wantSuffix:      "co.uk",
			wantRegistrable: "example.co.uk",
		},
		{
			name:            "uppercase is normalized",
			input:           "EXAMPLE.COM",
			wantSLD:         "example",
			wantSuffix:      "com",
			wantRegistrable: "example.com",
		},
		{
			name:            "trailing root dot is stripped",
			input:           "example.com.",
			wantSLD:         "example",
			wantSuffix:      "com",
			wantRegistrable: "example.com",
		},
		{
			name:            "surrounding whitespace is trimmed",
			input:           "  example.com  ",
			wantSLD:         "example",
			wantSuffix:      "com",
			wantRegistrable: "example.com",
		},
		{
			name:            "private suffix",
			input:           "example.github.io",
			wantSLD:         "example",
			wantSuffix:      "github.io",
			wantRegistrable: "example.github.io",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := engine.ParseSeed(tt.input)
			if err != nil {
				t.Fatalf("ParseSeed(%q): %v", tt.input, err)
			}
			if got.Subdomain != tt.wantSubdomain {
				t.Errorf("Subdomain = %q, want %q", got.Subdomain, tt.wantSubdomain)
			}
			if got.SLD != tt.wantSLD {
				t.Errorf("SLD = %q, want %q", got.SLD, tt.wantSLD)
			}
			if got.Suffix != tt.wantSuffix {
				t.Errorf("Suffix = %q, want %q", got.Suffix, tt.wantSuffix)
			}
			if got.Registrable() != tt.wantRegistrable {
				t.Errorf("Registrable() = %q, want %q", got.Registrable(), tt.wantRegistrable)
			}
		})
	}
}

func TestParseSeedErrors(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"whitespace only", "   "},
		{"no dot", "example"},
		{"url rather than domain", "https://example.com"},
		{"path appended", "example.com/path"},
		{"public suffix alone", "com"},
		{"multi-label public suffix alone", "co.uk"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := engine.ParseSeed(tt.input); err == nil {
				t.Errorf("ParseSeed(%q) succeeded, want an error", tt.input)
			}
		})
	}
}

func TestSeedString(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"example.com", "example.com"},
		{"www.example.com", "www.example.com"},
		{"WWW.Example.Com.", "www.example.com"},
	}

	for _, tt := range tests {
		seed, err := engine.ParseSeed(tt.input)
		if err != nil {
			t.Fatalf("ParseSeed(%q): %v", tt.input, err)
		}
		if got := seed.String(); got != tt.want {
			t.Errorf("String() = %q, want %q", got, tt.want)
		}
	}
}

func TestNormalizeDomain(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"  Example.COM.  ", "example.com"},
		{"münchen.de", "xn--mnchen-3ya.de"},
		{"xn--mnchen-3ya.de", "xn--mnchen-3ya.de"},
		{"exаmple.com", "xn--exmple-4nf.com"}, // Cyrillic а
		{"not-a-domain", "not-a-domain"},
	}
	for _, tt := range tests {
		if got := engine.NormalizeDomain(tt.input); got != tt.want {
			t.Errorf("NormalizeDomain(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
