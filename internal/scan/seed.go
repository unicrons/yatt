package scan

import "github.com/andoniaf/yatt/pkg/engine"

// IsOriginal reports whether this finding is the seed's own row rather than one
// of its look-alikes.
func (f Finding) IsOriginal() bool {
	return f.Technique == engine.TechniqueOriginal
}

// SeedFirst returns the findings with the seed's own row leading.
//
// The guarantee lives here, at the output boundary, rather than at the point the
// candidates are generated: enforcing it once per output format is what makes
// "the seed always comes first" true regardless of any sorting a caller applies
// to the candidates afterwards.
//
// The input is returned untouched when the seed already leads or is absent,
// which is the common case, so the ordinary path allocates nothing.
func SeedFirst(findings []Finding) []Finding {
	index := -1
	for i := range findings {
		if findings[i].IsOriginal() {
			index = i
			break
		}
	}
	if index <= 0 {
		return findings
	}

	out := make([]Finding, 0, len(findings))
	out = append(out, findings[index])
	out = append(out, findings[:index]...)
	return append(out, findings[index+1:]...)
}
