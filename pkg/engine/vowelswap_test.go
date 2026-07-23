package engine_test

import (
	"reflect"
	"testing"

	"github.com/unicrons/yatt/pkg/engine"
)

func TestVowelSwapPermute(t *testing.T) {
	tests := []struct {
		name string
		sld  string
		want []string
	}{
		{
			name: "each other vowel substituted in aeiou order",
			sld:  "cat",
			want: []string{"cet", "cit", "cot", "cut"},
		},
		{
			name: "each vowel position swapped in turn",
			sld:  "ae",
			want: []string{"ee", "ie", "oe", "ue", "aa", "ai", "ao", "au"},
		},
		{
			name: "repeated vowels each swapped independently",
			sld:  "aa",
			want: []string{"ea", "ia", "oa", "ua", "ae", "ai", "ao", "au"},
		},
		{
			name: "label with no vowels yields nothing",
			sld:  "xyz",
			want: nil,
		},
		{
			name: "y is not treated as a vowel",
			sld:  "gym",
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
			got := engine.VowelSwap{}.Permute(tt.sld)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Permute(%q) = %v, want %v", tt.sld, got, tt.want)
			}
		})
	}
}

func TestVowelSwapIsDeterministic(t *testing.T) {
	technique := engine.VowelSwap{}
	first := technique.Permute("example")
	for i := 0; i < 10; i++ {
		got := technique.Permute("example")
		if !reflect.DeepEqual(got, first) {
			t.Fatalf("run %d differed: %v != %v", i, got, first)
		}
	}
}
