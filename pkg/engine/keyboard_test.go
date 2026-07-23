package engine_test

import (
	"reflect"
	"testing"

	"github.com/unicrons/yatt/pkg/engine"
	"github.com/unicrons/yatt/pkg/engine/data"
)

func TestKeyboardPermuteContainsPhysicalNeighbors(t *testing.T) {
	// "z" on qwerty replaces with each of its physical neighbors a, s, x —
	// verified against data.Keyboards directly, so this test does not
	// hardcode a specific adjacency table and stays valid if the geometry
	// that derives it is retuned.
	got := engine.Keyboard{}.Permute("z")

	want := make(map[string]bool)
	for _, layout := range data.KeyboardLayoutNames {
		for _, n := range data.Keyboards[layout]['z'] {
			want[string(n)] = true
		}
	}
	if len(want) == 0 {
		t.Fatal("no layout has a neighbor for 'z' to compare against")
	}

	gotSet := make(map[string]bool, len(got))
	for _, v := range got {
		gotSet[v] = true
	}
	for w := range want {
		if !gotSet[w] {
			t.Errorf("Permute(\"z\") = %v, missing physical neighbor %q", got, w)
		}
	}
}

func TestKeyboardPermuteReplacesEachPosition(t *testing.T) {
	got := engine.Keyboard{}.Permute("ab")
	if len(got) == 0 {
		t.Fatal("no variants produced for a two-character label")
	}
	for _, v := range got {
		if len([]rune(v)) != 2 {
			t.Errorf("variant %q changed length, want a same-length replacement", v)
		}
	}
}

func TestKeyboardPermuteDedupes(t *testing.T) {
	got := engine.Keyboard{}.Permute("aa")
	seen := make(map[string]bool, len(got))
	for _, v := range got {
		if seen[v] {
			t.Errorf("duplicate variant %q", v)
		}
		seen[v] = true
	}
}

func TestKeyboardPermuteEmptyLabelYieldsNothing(t *testing.T) {
	if got := (engine.Keyboard{}).Permute(""); got != nil {
		t.Errorf("Permute(\"\") = %v, want nil", got)
	}
}

func TestKeyboardIsDeterministic(t *testing.T) {
	technique := engine.Keyboard{}
	first := technique.Permute("example")
	for i := 0; i < 10; i++ {
		got := technique.Permute("example")
		if !reflect.DeepEqual(got, first) {
			t.Fatalf("run %d differed: %v != %v", i, got, first)
		}
	}
}

func TestKeyboardName(t *testing.T) {
	if got := (engine.Keyboard{}).Name(); got != "keyboard" {
		t.Errorf("Name() = %q, want %q", got, "keyboard")
	}
}
