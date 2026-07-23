package engine_test

import (
	"reflect"
	"testing"

	"github.com/unicrons/yatt/pkg/engine"
)

func TestBitsquattingPermute(t *testing.T) {
	tests := []struct {
		name string
		sld  string
		want []string
	}{
		{
			// 'a' (0x61) survives masks 2,4,8,16 → c,e,i,q; 'b' (0x62)
			// survives 1,4,8,16 → c,f,j,r. Position outer, mask ascending
			// inner — dnstwist's order.
			name: "flips each character position left to right",
			sld:  "ab",
			want: []string{"cb", "eb", "ib", "qb", "ac", "af", "aj", "ar"},
		},
		{
			name: "single character label",
			sld:  "a",
			want: []string{"c", "e", "i", "q"},
		},
		{
			// '0' (0x30) survives masks 1,2,4,8,64: digits flip into both
			// digits and letters.
			name: "digit label",
			sld:  "0",
			want: []string{"1", "2", "4", "8", "p"},
		},
		{
			// 'm' (0x6d) ^ 64 = '-': hyphen variants are emitted here and
			// left for ValidLabel to reject when they land on an edge.
			name: "a flip may produce a hyphen",
			sld:  "m",
			want: []string{"l", "o", "i", "e", "-"},
		},
		{
			// A single-bit flip on a non-ASCII rune never lands in the
			// hostname set, so only the ASCII position produces variants.
			name: "non-ASCII runes are skipped",
			sld:  "aé",
			want: []string{"cé", "eé", "ié", "qé"},
		},
		{
			name: "empty label yields nothing",
			sld:  "",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := engine.Bitsquatting{}.Permute(tt.sld)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Permute(%q) = %q, want %q", tt.sld, got, tt.want)
			}
		})
	}
}

// TestBitsquattingFlipsTheFinalCharacter pins two variants that differ from
// the seed by a single bit in the last character — the flip shape a dnstwist
// comparison showed yatt could not generate.
func TestBitsquattingFlipsTheFinalCharacter(t *testing.T) {
	got := engine.Bitsquatting{}.Permute("unicrons")

	want := map[string]bool{"unicronc": false, "unicronw": false}
	for _, variant := range got {
		if _, ok := want[variant]; ok {
			want[variant] = true
		}
	}
	for variant, found := range want {
		if !found {
			t.Errorf("Permute(\"unicrons\") did not produce %q", variant)
		}
	}
}

func TestBitsquattingNeverRepeatsAVariant(t *testing.T) {
	got := engine.Bitsquatting{}.Permute("aa")

	seen := make(map[string]bool, len(got))
	for _, variant := range got {
		if seen[variant] {
			t.Errorf("variant %q appeared twice", variant)
		}
		seen[variant] = true
	}
}

func TestBitsquattingIsDeterministic(t *testing.T) {
	technique := engine.Bitsquatting{}
	first := technique.Permute("example")
	for i := 0; i < 10; i++ {
		got := technique.Permute("example")
		if !reflect.DeepEqual(got, first) {
			t.Fatalf("run %d differed: %v != %v", i, got, first)
		}
	}
}
