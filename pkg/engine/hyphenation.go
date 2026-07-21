package engine

func init() {
	Register(Hyphenation{})
}

// Hyphenation generates the variants produced by inserting a hyphen between
// two adjacent characters, modelling the squatter pattern of registering the
// word-split spelling of a compound name ("uni-crons.cloud" for
// "unicrons.cloud").
type Hyphenation struct{}

// Name implements Technique.
func (Hyphenation) Name() string { return "hyphenation" }

// Permute inserts '-' at each interior position in turn, returning a
// deduplicated slice in left-to-right index order. Positions where either
// neighbour is already a hyphen are skipped: they would produce "--" runs or
// duplicates of the label itself. Interior-only insertion means no variant
// gains a leading or trailing hyphen, so ValidLabel never rejects one.
func (Hyphenation) Permute(sld string) []string {
	runes := []rune(sld)
	if len(runes) < 2 {
		return nil
	}

	out := make([]string, 0, len(runes)-1)
	seen := make(map[string]bool, len(runes)-1)
	for i := 1; i < len(runes); i++ {
		if runes[i-1] == '-' || runes[i] == '-' {
			continue
		}
		variant := string(runes[:i]) + "-" + string(runes[i:])
		if seen[variant] {
			continue
		}
		seen[variant] = true
		out = append(out, variant)
	}
	return out
}
