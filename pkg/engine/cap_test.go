package engine_test

import (
	"testing"

	"github.com/unicrons/yatt/pkg/engine"
)

func makeCandidates(technique string, n int) []engine.Candidate {
	out := make([]engine.Candidate, n)
	for i := range out {
		out[i] = engine.Candidate{
			Domain:    technique + string(rune('a'+i%26)) + ".com",
			Technique: technique,
		}
	}
	return out
}

func TestCapPerTechnique(t *testing.T) {
	// Two label-mutating techniques, so both are subject to the cap; tld is a
	// SuffixSwap and exempt, which TestCapExemptsSuffixSwapTechniques covers.
	var candidates []engine.Candidate
	candidates = append(candidates, makeCandidates("omission", 10)...)
	candidates = append(candidates, makeCandidates("keyboard", 10)...)

	got := engine.Cap(candidates, 3, 0)

	counts := map[string]int{}
	for _, c := range got {
		counts[c.Technique]++
	}
	if counts["omission"] != 3 {
		t.Errorf("omission count = %d, want 3", counts["omission"])
	}
	if counts["keyboard"] != 3 {
		t.Errorf("keyboard count = %d, want 3", counts["keyboard"])
	}
}

func TestCapExemptsSuffixSwapTechniques(t *testing.T) {
	// tld is a SuffixSwap, so its whole contribution passes the per-technique
	// cap; omission, a label technique, is still capped.
	var candidates []engine.Candidate
	candidates = append(candidates, makeCandidates("omission", 10)...)
	candidates = append(candidates, makeCandidates("tld", 10)...)

	got := engine.Cap(candidates, 3, 0)

	counts := map[string]int{}
	for _, c := range got {
		counts[c.Technique]++
	}
	if counts["omission"] != 3 {
		t.Errorf("omission count = %d, want 3 (still capped)", counts["omission"])
	}
	if counts["tld"] != 10 {
		t.Errorf("tld count = %d, want 10 (exempt from the technique cap)", counts["tld"])
	}
}

func TestCapGlobalLimitStillBoundsSuffixSwapTechniques(t *testing.T) {
	candidates := makeCandidates("tld", 10)

	if got := engine.Cap(candidates, 3, 5); len(got) != 5 {
		t.Errorf("got %d candidates, want 5: the global limit applies even to exempt techniques", len(got))
	}
}

func TestCapPerTechniquePreservesOrder(t *testing.T) {
	candidates := makeCandidates("omission", 5)
	got := engine.Cap(candidates, 3, 0)
	for i := 0; i < 3; i++ {
		if got[i] != candidates[i] {
			t.Errorf("got[%d] = %+v, want %+v — capping must keep the nearest-first candidates", i, got[i], candidates[i])
		}
	}
}

func TestCapZeroTechniqueCapIsUnlimited(t *testing.T) {
	candidates := makeCandidates("omission", 50)
	got := engine.Cap(candidates, 0, 0)
	if len(got) != 50 {
		t.Errorf("got %d candidates, want all 50", len(got))
	}
}

func TestCapGlobalLimit(t *testing.T) {
	var candidates []engine.Candidate
	candidates = append(candidates, makeCandidates("omission", 10)...)
	candidates = append(candidates, makeCandidates("tld", 10)...)

	got := engine.Cap(candidates, 0, 5)
	if len(got) != 5 {
		t.Fatalf("got %d candidates, want 5", len(got))
	}
	for i := 0; i < 5; i++ {
		if got[i] != candidates[i] {
			t.Errorf("got[%d] = %+v, want the first five in technique order", i, got[i])
		}
	}
}

func TestCapZeroLimitIsUnlimited(t *testing.T) {
	candidates := makeCandidates("omission", 50)
	got := engine.Cap(candidates, 0, 0)
	if len(got) != 50 {
		t.Errorf("got %d candidates, want all 50", len(got))
	}
}

func TestCapAppliesTechniqueCapBeforeGlobalLimit(t *testing.T) {
	var candidates []engine.Candidate
	candidates = append(candidates, makeCandidates("omission", 10)...)
	candidates = append(candidates, makeCandidates("keyboard", 10)...)

	// Technique cap 3 leaves 3 omission + 3 keyboard = 6; global limit 4 then
	// truncates that combined list, not the original 20.
	got := engine.Cap(candidates, 3, 4)
	if len(got) != 4 {
		t.Fatalf("got %d candidates, want 4", len(got))
	}
	for _, c := range got[:3] {
		if c.Technique != "omission" {
			t.Errorf("candidate %+v, want the first 3 to be omission", c)
		}
	}
	if got[3].Technique != "keyboard" {
		t.Errorf("4th candidate = %+v, want the first keyboard candidate", got[3])
	}
}
