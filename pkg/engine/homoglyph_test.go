package engine_test

import (
	"reflect"
	"testing"

	"github.com/unicrons/yatt/pkg/engine"
)

func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

func TestHomoglyphPermuteTwoCharWindowFires(t *testing.T) {
	// "rn" -> "m" only fires from the two-character window: the label has
	// no single character that maps to "m" on its own.
	got := engine.Homoglyph{}.Permute("modern")
	if !contains(got, "modem") {
		t.Errorf("Permute(\"modern\") = %v, want it to contain \"modem\" (rn -> m)", got)
	}
}

func TestHomoglyphPermuteSingleCharSubstitutionFires(t *testing.T) {
	// "m" -> "rn" is the reverse direction, from the same table entry.
	got := engine.Homoglyph{}.Permute("modem")
	if !contains(got, "modern") {
		t.Errorf("Permute(\"modem\") = %v, want it to contain \"modern\" (m -> rn)", got)
	}
}

func TestHomoglyphPermuteASCIIDigitSubstitution(t *testing.T) {
	got := engine.Homoglyph{}.Permute("google")
	if !contains(got, "g00gle") {
		t.Errorf("Permute(\"google\") = %v, want it to contain \"g00gle\" (compounded o -> 0 twice)", got)
	}
}

func TestHomoglyphPermuteUnicodeSubstitution(t *testing.T) {
	got := engine.Homoglyph{}.Permute("apple")
	// Cyrillic "а" (U+0430) replacing the Latin "a".
	if !contains(got, "аpple") {
		t.Errorf("Permute(\"apple\") did not contain the Cyrillic-a variant")
	}
}

func TestHomoglyphPermuteNeverReturnsTheInputUnchanged(t *testing.T) {
	got := engine.Homoglyph{}.Permute("google")
	if contains(got, "google") {
		t.Error("Permute(\"google\") contains the unchanged input")
	}
}

func TestHomoglyphPermuteDedupes(t *testing.T) {
	got := engine.Homoglyph{}.Permute("google")
	seen := make(map[string]bool, len(got))
	for _, v := range got {
		if seen[v] {
			t.Errorf("duplicate variant %q", v)
		}
		seen[v] = true
	}
}

func TestHomoglyphPermuteWithSuffixGatesUnicode(t *testing.T) {
	ungated := engine.Homoglyph{}.PermuteWithSuffix("apple", "com")
	gated := engine.Homoglyph{}.PermuteWithSuffix("apple", "us")

	if !contains(ungated, "аpple") {
		t.Fatalf("PermuteWithSuffix(%q, %q) does not contain the Cyrillic-a variant, want it present for an ungated TLD", "apple", "com")
	}
	if contains(gated, "аpple") {
		t.Errorf("PermuteWithSuffix(%q, %q) contains a Unicode variant, want it suppressed for a gated TLD", "apple", "us")
	}
	// ASCII substitutions are never gated: they need no IDN support.
	if !contains(gated, "appl3") {
		t.Errorf("PermuteWithSuffix(%q, %q) = %v, want the ASCII substitution to still fire", "apple", "us", gated)
	}

	// The gate keys bare TLDs but a public suffix is often multi-label: the
	// registry policy that gates "example.jp" gates "example.co.jp" too.
	if gatedMulti := (engine.Homoglyph{}).PermuteWithSuffix("apple", "co.jp"); contains(gatedMulti, "аpple") {
		t.Errorf("PermuteWithSuffix(%q, %q) contains a Unicode variant, want the gate to cover a multi-label suffix under a gated ccTLD", "apple", "co.jp")
	}
}

func TestHomoglyphPermuteWithoutSuffixUsesTheFullUnicodeTable(t *testing.T) {
	viaPermute := engine.Homoglyph{}.Permute("apple")
	viaEmptySuffix := engine.Homoglyph{}.PermuteWithSuffix("apple", "")
	if !reflect.DeepEqual(viaPermute, viaEmptySuffix) {
		t.Errorf("Permute and PermuteWithSuffix(_, \"\") disagree:\n Permute:            %v\n PermuteWithSuffix: %v",
			viaPermute, viaEmptySuffix)
	}
}

func TestHomoglyphIsDeterministic(t *testing.T) {
	technique := engine.Homoglyph{}
	first := technique.Permute("example")
	for i := 0; i < 10; i++ {
		got := technique.Permute("example")
		if !reflect.DeepEqual(got, first) {
			t.Fatalf("run %d differed: %v != %v", i, got, first)
		}
	}
}

func TestHomoglyphName(t *testing.T) {
	if got := (engine.Homoglyph{}).Name(); got != "homoglyph" {
		t.Errorf("Name() = %q, want %q", got, "homoglyph")
	}
}
