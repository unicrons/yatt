package engine_test

import (
	"reflect"
	"testing"

	"github.com/andoniaf/yatt/pkg/engine"
)

func TestDotInsertionPermuteSplit(t *testing.T) {
	tests := []struct {
		name string
		sld  string
		want [][2]string
	}{
		{
			// The dot walks left to right; the tail always keeps at least two
			// characters — dnstwist's range.
			name: "splits at each interior position",
			sld:  "abcd",
			want: [][2]string{{"a", "bcd"}, {"ab", "cd"}},
		},
		{
			// Each split turns the tail into its own registrable domain —
			// uni.crons yields crons as the name a squatter registers.
			name: "walks the dot through a longer label",
			sld:  "unicrons",
			want: [][2]string{
				{"u", "nicrons"}, {"un", "icrons"}, {"uni", "crons"},
				{"unic", "rons"}, {"unicr", "ons"}, {"unicro", "ns"},
			},
		},
		{
			// A dot next to a hyphen would leave a hyphen-edged label, so both
			// positions around one are skipped.
			name: "skips positions adjacent to a hyphen",
			sld:  "ab-cd",
			want: [][2]string{{"a", "b-cd"}},
		},
		{
			name: "a label that is all hyphen-adjacent yields nothing",
			sld:  "a-bc",
			want: [][2]string{},
		},
		{
			name: "two characters cannot split",
			sld:  "ab",
			want: nil,
		},
		{
			name: "empty label yields nothing",
			sld:  "",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := engine.DotInsertion{}.PermuteSplit(tt.sld)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("PermuteSplit(%q) = %v, want %v", tt.sld, got, tt.want)
			}
		})
	}
}

func TestDotInsertionPermuteReturnsNil(t *testing.T) {
	// Permute exists only to satisfy Technique: a dotted SLD is not a label,
	// so there is no SLD-only answer to give.
	if got := (engine.DotInsertion{}).Permute("example"); got != nil {
		t.Errorf("Permute = %v, want nil", got)
	}
}

func TestDotInsertionNeverRepeatsASplit(t *testing.T) {
	got := engine.DotInsertion{}.PermuteSplit("aaaa")

	seen := make(map[[2]string]bool, len(got))
	for _, split := range got {
		if seen[split] {
			t.Errorf("split %v appeared twice", split)
		}
		seen[split] = true
	}
}

func TestDotInsertionIsDeterministic(t *testing.T) {
	technique := engine.DotInsertion{}
	first := technique.PermuteSplit("example")
	for i := 0; i < 10; i++ {
		got := technique.PermuteSplit("example")
		if !reflect.DeepEqual(got, first) {
			t.Fatalf("run %d differed: %v != %v", i, got, first)
		}
	}
}
