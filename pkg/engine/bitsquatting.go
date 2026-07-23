package engine

func init() {
	Register(Bitsquatting{})
}

// Bitsquatting generates the variants produced by flipping a single bit in one
// character, modelling the squatter pattern of registering names that a memory
// error — not a typo — turns the real domain into ("unicrons.cloud" reached as
// "unicronc.cloud").
type Bitsquatting struct{}

// Name implements Technique.
func (Bitsquatting) Name() string { return "bitsquatting" }

// Permute XORs each character with each of the eight single-bit masks,
// position by position left to right and mask ascending within a position —
// the same order dnstwist uses, so side-by-side comparisons line up. Only
// flips that land in the hostname character set survive; non-ASCII runes are
// skipped entirely, since a single-bit flip on one never lands there.
func (Bitsquatting) Permute(sld string) []string {
	runes := []rune(sld)
	if len(runes) == 0 {
		return nil
	}

	var out []string
	seen := make(map[string]bool)
	for i, r := range runes {
		if r > 127 {
			continue
		}
		for mask := rune(1); mask <= 128; mask <<= 1 {
			flipped := r ^ mask
			if !hostnameRune(flipped) {
				continue
			}
			variant := string(runes[:i]) + string(flipped) + string(runes[i+1:])
			if seen[variant] {
				continue
			}
			seen[variant] = true
			out = append(out, variant)
		}
	}
	return out
}

// hostnameRune reports whether r is a character a hostname label may contain.
// Hyphen-edged variants are still possible here; ValidLabel rejects them
// downstream, as it does for every technique.
func hostnameRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-'
}
