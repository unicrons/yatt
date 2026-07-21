package engine_test

import (
	"reflect"
	"testing"

	"github.com/andoniaf/yatt/pkg/engine"
)

func TestTranspositionPermute(t *testing.T) {
	tests := []struct {
		name string
		sld  string
		want []string
	}{
		{
			name: "each adjacent pair swapped in turn",
			sld:  "abcd",
			want: []string{"bacd", "acbd", "abdc"},
		},
		{
			name: "swapping identical adjacent characters reproduces the input",
			sld:  "aab",
			want: []string{"aab", "aba"},
		},
		{
			name: "two character label yields one swap",
			sld:  "ab",
			want: []string{"ba"},
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
			// A byte-wise implementation would swap halves of the two-byte "à"
			// with its neighbor and emit invalid UTF-8; the rune-wise one swaps
			// it whole.
			name: "multibyte runes are swapped whole, not by byte",
			sld:  "exàmple",
			want: []string{"xeàmple", "eàxmple", "exmàple", "exàpmle", "exàmlpe", "exàmpel"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := engine.Transposition{}.Permute(tt.sld)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Permute(%q) = %v, want %v", tt.sld, got, tt.want)
			}
		})
	}
}

func TestTranspositionIsDeterministic(t *testing.T) {
	technique := engine.Transposition{}
	first := technique.Permute("example")
	for i := 0; i < 10; i++ {
		got := technique.Permute("example")
		if !reflect.DeepEqual(got, first) {
			t.Fatalf("run %d differed: %v != %v", i, got, first)
		}
	}
}

func TestTranspositionName(t *testing.T) {
	if got := (engine.Transposition{}).Name(); got != "transposition" {
		t.Errorf("Name() = %q, want %q", got, "transposition")
	}
}
