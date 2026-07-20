package engine_test

import (
	"encoding/json"
	"testing"

	"github.com/andoniaf/yatt/pkg/engine"
)

// TestPermuteAllTechniquesIsDeterministic guards the property cap.go's
// truncation and the whole cross-scan diff feature depend on: running every
// technique over a fixed seed produces byte-identical output every time.
func TestPermuteAllTechniquesIsDeterministic(t *testing.T) {
	seed, err := engine.ParseSeed("example.com")
	if err != nil {
		t.Fatalf("ParseSeed: %v", err)
	}

	var first []byte
	for run := 0; run < 5; run++ {
		candidates := engine.Cap(engine.Permute(seed, engine.All()), engine.DefaultTechniqueCap, 0)
		encoded, err := json.Marshal(candidates)
		if err != nil {
			t.Fatalf("run %d: marshal: %v", run, err)
		}
		if run == 0 {
			first = encoded
			continue
		}
		if string(encoded) != string(first) {
			t.Fatalf("run %d differs from run 0", run)
		}
	}
}

// TestPermuteAllTechniquesOrdersNearestFirst asserts the ordering cap.go's
// truncation relies on: the single-character-edit techniques
// (omission, transposition, keyboard) lead the candidate list, ahead of the
// two fan-out techniques (tld, homoglyph).
func TestPermuteAllTechniquesOrdersNearestFirst(t *testing.T) {
	seed, err := engine.ParseSeed("example.com")
	if err != nil {
		t.Fatalf("ParseSeed: %v", err)
	}

	candidates := engine.Permute(seed, engine.All())
	if len(candidates) == 0 {
		t.Fatal("no candidates produced")
	}

	nearest := map[string]bool{"omission": true, "transposition": true, "keyboard": true}
	fanOut := map[string]bool{"tld": true, "homoglyph": true}

	seenFanOut := false
	for _, c := range candidates {
		switch {
		case fanOut[c.Technique]:
			seenFanOut = true
		case nearest[c.Technique]:
			if seenFanOut {
				t.Fatalf("candidate %+v from a nearest-first technique appeared after a fan-out technique", c)
			}
		}
	}
}

// TestPermuteAllTechniquesIncludesEveryRegisteredTechnique guards against a
// technique silently failing to produce anything for a realistic seed — a
// technique registered but never actually exercised by the default run
// would be dead code with nobody noticing.
func TestPermuteAllTechniquesIncludesEveryRegisteredTechnique(t *testing.T) {
	seed, err := engine.ParseSeed("example.com")
	if err != nil {
		t.Fatalf("ParseSeed: %v", err)
	}

	seen := make(map[string]bool)
	for _, c := range engine.Permute(seed, engine.All()) {
		seen[c.Technique] = true
	}
	for _, name := range engine.Names() {
		if !seen[name] {
			t.Errorf("technique %q produced no candidates for example.com", name)
		}
	}
}

// TestSelectRespectsCanonicalOrderRegardlessOfInputOrder guards the
// property --technique flags rely on: the order names are typed in must
// never change the candidate list's order.
func TestSelectRespectsCanonicalOrderRegardlessOfInputOrder(t *testing.T) {
	forward, err := engine.Select([]string{"omission", "homoglyph", "tld"})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	backward, err := engine.Select([]string{"tld", "homoglyph", "omission"})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}

	if len(forward) != len(backward) {
		t.Fatalf("forward has %d techniques, backward has %d", len(forward), len(backward))
	}
	for i := range forward {
		if forward[i].Name() != backward[i].Name() {
			t.Errorf("position %d: forward=%q backward=%q, want the same regardless of input order",
				i, forward[i].Name(), backward[i].Name())
		}
	}
}
