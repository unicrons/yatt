package engine

func init() {
	Register(Transposition{})
}

// Transposition generates the variants produced by swapping two adjacent
// characters, modelling the typo of hitting keys in the wrong order — not
// the exhaustive "move any character to any position" reordering, which
// produces mostly unrealistic typos for the number of candidates it costs.
type Transposition struct{}

// Name implements Technique.
func (Transposition) Name() string { return "transposition" }

// Permute swaps each adjacent pair of characters in turn, returning a
// deduplicated slice in left-to-right index order.
func (Transposition) Permute(sld string) []string {
	runes := []rune(sld)
	if len(runes) < 2 {
		return nil
	}

	out := make([]string, 0, len(runes)-1)
	seen := make(map[string]bool, len(runes)-1)
	for i := 0; i < len(runes)-1; i++ {
		swapped := append([]rune(nil), runes...)
		swapped[i], swapped[i+1] = swapped[i+1], swapped[i]
		variant := string(swapped)
		if seen[variant] {
			continue
		}
		seen[variant] = true
		out = append(out, variant)
	}
	return out
}
