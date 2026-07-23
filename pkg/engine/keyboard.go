package engine

import "github.com/unicrons/yatt/pkg/engine/data"

func init() {
	Register(Keyboard{})
}

// Keyboard generates the variants produced by replacing a character with one
// physically next to it on a keyboard — the typo of a slipped finger, rather
// than a visual or phonetic confusion. It checks every layout in
// data.Keyboards (qwerty, qwertz, azerty) and unions the results, since a
// scan has no way to know which keyboard a squatter typed the candidate on.
type Keyboard struct{}

// Name implements Technique.
func (Keyboard) Name() string { return "keyboard" }

// Permute replaces each character in turn with each of its neighbors on
// every known layout, returning a deduplicated slice. Layouts are walked in
// data.KeyboardLayoutNames order and positions left to right, so the result
// is stable however Go happens to range over the underlying maps.
func (Keyboard) Permute(sld string) []string {
	runes := []rune(sld)
	if len(runes) == 0 {
		return nil
	}

	var out []string
	seen := make(map[string]bool)
	for _, layout := range data.KeyboardLayoutNames {
		adjacency := data.Keyboards[layout]
		for i, r := range runes {
			for _, n := range adjacency[r] {
				variant := string(runes[:i]) + string(n) + string(runes[i+1:])
				if seen[variant] {
					continue
				}
				seen[variant] = true
				out = append(out, variant)
			}
		}
	}
	return out
}
