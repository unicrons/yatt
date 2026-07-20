package engine

// DefaultTechniqueCap bounds how many candidates a single technique may
// contribute, so one fan-out-heavy technique (homoglyph's two compounded
// substitution passes, TLD swap sweeping a large list) cannot crowd out
// every other technique's candidates or, on the "full" TLD profile, turn an
// ordinary scan into a multi-thousand-query one by itself.
const DefaultTechniqueCap = 500

// Cap bounds a candidate set two ways: first each technique's own
// contribution is truncated to techniqueCap (zero or negative means
// unlimited), in the order that technique produced them; then the combined,
// technique-order-preserving list is truncated to limit (zero or negative
// means unlimited).
//
// Every technique here produces its own output nearest-first — omission and
// transposition are a single edit by construction, keyboard tries the
// nearest key before a second layout's, homoglyph's first substitution pass
// precedes its compounded second pass, and TLD swap leads with the seed's
// own near-misses — so truncating rather than sampling keeps the closest
// look-alikes and, just as importantly, keeps two scans of the same seed
// byte-identical, which the diff feature depends on.
func Cap(candidates []Candidate, techniqueCap, limit int) []Candidate {
	if techniqueCap > 0 {
		candidates = capPerTechnique(candidates, techniqueCap)
	}
	if limit > 0 && len(candidates) > limit {
		candidates = candidates[:limit]
	}
	return candidates
}

// capPerTechnique truncates each technique's contribution independently,
// preserving the relative order of the techniques and of each technique's
// own candidates.
func capPerTechnique(candidates []Candidate, cap int) []Candidate {
	counts := make(map[string]int)
	out := make([]Candidate, 0, len(candidates))
	for _, c := range candidates {
		if counts[c.Technique] >= cap {
			continue
		}
		counts[c.Technique]++
		out = append(out, c)
	}
	return out
}
