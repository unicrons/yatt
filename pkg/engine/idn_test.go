package engine_test

import (
	"testing"

	"golang.org/x/net/idna"

	"github.com/andoniaf/yatt/pkg/engine"
)

func TestToASCII(t *testing.T) {
	tests := []struct {
		name      string
		label     string
		wantASCII string
		wantOK    bool
	}{
		{
			name:      "plain ASCII passes through unchanged",
			label:     "xample",
			wantASCII: "xample",
			wantOK:    true,
		},
		{
			name:      "a Cyrillic homoglyph becomes its punycode form",
			label:     "gоogle", // Cyrillic 'о' (U+043E) replacing the first 'o'
			wantASCII: "xn--gogle-jye",
			wantOK:    true,
		},
		{
			name:      "hyphens in the ACE prefix position do not get rejected",
			label:     "ab--cd",
			wantASCII: "ab--cd",
			wantOK:    true,
		},
		{
			// UTS 46 mapping folds case before punycoding. Without it the
			// bare Punycode profile encodes "Зoom" as "xn--oom-b9c", a wire
			// form no registry uses — the actually-registrable homograph is
			// the lowercased one, and querying the wrong form reports a real
			// registered look-alike as free.
			name:      "an uppercase confusable maps to the registrable lowercase form",
			label:     "Зoom", // Cyrillic 'З' (U+0417) replacing the 'Z'
			wantASCII: "xn--oom-ydd",
			wantOK:    true,
		},
		{
			// Fullwidth forms map back to their ASCII originals under UTS 46,
			// so a fullwidth substitution collapses to the seed itself and is
			// dropped by addCandidate instead of being queried as junk.
			name:      "a fullwidth form maps back to plain ASCII",
			label:     "ｅxample", // fullwidth 'ｅ' (U+FF45)
			wantASCII: "example",
			wantOK:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ascii, ok := engine.ToASCII(tt.label)
			if ok != tt.wantOK {
				t.Fatalf("ToASCII(%q) ok = %v, want %v", tt.label, ok, tt.wantOK)
			}
			if ascii != tt.wantASCII {
				t.Errorf("ToASCII(%q) = %q, want %q", tt.label, ascii, tt.wantASCII)
			}
		})
	}
}

// TestToASCIIRoundTrips guards the property idn.go's own doc comment relies
// on: a punycode-encoded label decodes back to the Unicode form it started
// from.
func TestToASCIIRoundTrips(t *testing.T) {
	original := "gоogle"

	ascii, ok := engine.ToASCII(original)
	if !ok {
		t.Fatalf("ToASCII(%q) failed", original)
	}

	unicode, err := idna.ToUnicode(ascii)
	if err != nil {
		t.Fatalf("ToUnicode(%q): %v", ascii, err)
	}
	if unicode != original {
		t.Errorf("round trip = %q, want %q", unicode, original)
	}
}
