package engine_test

import (
	"reflect"
	"testing"

	"github.com/andoniaf/yatt/pkg/engine"
)

func TestHyphenationPermute(t *testing.T) {
	tests := []struct {
		name string
		sld  string
		want []string
	}{
		{
			name: "hyphen inserted at each interior position",
			sld:  "abc",
			want: []string{"a-bc", "ab-c"},
		},
		{
			name: "positions adjacent to an existing hyphen are skipped",
			sld:  "a-bc",
			want: []string{"a-b-c"},
		},
		{
			name: "two character label yields the single split",
			sld:  "ab",
			want: []string{"a-b"},
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
			// invalid UTF-8 here; the rune-wise one inserts between whole runes.
			name: "multibyte runes are kept whole",
			sld:  "àb",
			want: []string{"à-b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := engine.Hyphenation{}.Permute(tt.sld)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Permute(%q) = %v, want %v", tt.sld, got, tt.want)
			}
		})
	}
}

func TestHyphenationNeverProducesEdgeHyphens(t *testing.T) {
	for _, sld := range []string{"example", "a-b", "ab-cd", "unicrons"} {
		for _, variant := range (engine.Hyphenation{}).Permute(sld) {
			if !engine.ValidLabel(variant) {
				t.Errorf("Permute(%q) produced invalid label %q", sld, variant)
			}
		}
	}
}

func TestHyphenationIsDeterministic(t *testing.T) {
	technique := engine.Hyphenation{}
	first := technique.Permute("example")
	for i := 0; i < 10; i++ {
		got := technique.Permute("example")
		if !reflect.DeepEqual(got, first) {
			t.Fatalf("run %d differed: %v != %v", i, got, first)
		}
	}
}
