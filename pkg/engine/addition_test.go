package engine_test

import (
	"reflect"
	"testing"

	"github.com/andoniaf/yatt/pkg/engine"
)

func TestAdditionPermute(t *testing.T) {
	tests := []struct {
		name string
		sld  string
		want []string
	}{
		{
			name: "appends digits then letters in order",
			sld:  "ab",
			want: []string{
				"ab0", "ab1", "ab2", "ab3", "ab4", "ab5", "ab6", "ab7", "ab8", "ab9",
				"aba", "abb", "abc", "abd", "abe", "abf", "abg", "abh", "abi", "abj",
				"abk", "abl", "abm", "abn", "abo", "abp", "abq", "abr", "abs", "abt",
				"abu", "abv", "abw", "abx", "aby", "abz",
			},
		},
		{
			name: "single character label still gains every suffix",
			sld:  "a",
			want: []string{
				"a0", "a1", "a2", "a3", "a4", "a5", "a6", "a7", "a8", "a9",
				"aa", "ab", "ac", "ad", "ae", "af", "ag", "ah", "ai", "aj",
				"ak", "al", "am", "an", "ao", "ap", "aq", "ar", "as", "at",
				"au", "av", "aw", "ax", "ay", "az",
			},
		},
		{
			name: "empty label yields nothing",
			sld:  "",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := engine.Addition{}.Permute(tt.sld)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Permute(%q) = %v, want %v", tt.sld, got, tt.want)
			}
		})
	}
}

func TestAdditionIsDeterministic(t *testing.T) {
	technique := engine.Addition{}
	first := technique.Permute("example")
	for i := 0; i < 10; i++ {
		got := technique.Permute("example")
		if !reflect.DeepEqual(got, first) {
			t.Fatalf("run %d differed: %v != %v", i, got, first)
		}
	}
}
