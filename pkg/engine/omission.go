package engine

func init() {
	Register(Omission{})
}

// Omission generates the variants produced by dropping exactly one character,
// modelling the typo of a key that never registered.
type Omission struct{}

// Name implements Technique.
func (Omission) Name() string { return "omission" }

// Permute deletes the character at each position in turn, returning a
// deduplicated slice in left-to-right index order. Repeated characters collapse
// naturally: "google" yields "gogle" once, not twice.
func (Omission) Permute(sld string) []string {
	runes := []rune(sld)
	if len(runes) < 2 {
		return nil
	}

	out := make([]string, 0, len(runes))
	seen := make(map[string]bool, len(runes))
	for i := range runes {
		variant := string(runes[:i]) + string(runes[i+1:])
		if variant == "" || seen[variant] {
			continue
		}
		seen[variant] = true
		out = append(out, variant)
	}
	return out
}
