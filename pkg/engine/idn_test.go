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
