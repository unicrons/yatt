package engine

func init() {
	Register(VowelSwap{})
}

// vowels is the set vowel-swap substitutes between. It deliberately matches
// dnstwist's set (no 'y') so side-by-side comparisons line up.
const vowels = "aeiou"

// VowelSwap generates the variants produced by replacing one vowel with
// another, modelling both the typo of a mis-hit vowel and the squatter
// pattern of registering the near-homophone spelling ("unicrans.cloud" for
// "unicrons.cloud").
type VowelSwap struct{}

// Name implements Technique.
func (VowelSwap) Name() string { return "vowel-swap" }

// Permute substitutes each other vowel at each vowel position in turn,
// returning a deduplicated slice in left-to-right index order with the
// replacement vowels in "aeiou" order. A label with no vowels yields nothing.
func (VowelSwap) Permute(sld string) []string {
	runes := []rune(sld)
	if len(runes) < 1 {
		return nil
	}

	var out []string
	seen := make(map[string]bool)
	for i, r := range runes {
		if !isVowel(r) {
			continue
		}
		for _, v := range vowels {
			if v == r {
				continue
			}
			swapped := append([]rune(nil), runes...)
			swapped[i] = v
			variant := string(swapped)
			if seen[variant] {
				continue
			}
			seen[variant] = true
			out = append(out, variant)
		}
	}
	return out
}

func isVowel(r rune) bool {
	for _, v := range vowels {
		if r == v {
			return true
		}
	}
	return false
}
