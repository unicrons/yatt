package engine

func init() {
	Register(Addition{})
}

// Addition generates the variants produced by appending a single character to
// the label, modelling both the fat-finger typo of an extra trailing key and
// the squatter pattern of registering the pluralised name ("examples.com").
type Addition struct{}

// Name implements Technique.
func (Addition) Name() string { return "addition" }

// Permute appends each of '0'-'9' then 'a'-'z' to the label, in that order —
// the same order dnstwist uses, so side-by-side comparisons line up. Variants
// that would overflow the 63-character label limit are rejected downstream by
// ValidLabel.
func (Addition) Permute(sld string) []string {
	if len([]rune(sld)) == 0 {
		return nil
	}

	out := make([]string, 0, 36)
	for c := '0'; c <= '9'; c++ {
		out = append(out, sld+string(c))
	}
	for c := 'a'; c <= 'z'; c++ {
		out = append(out, sld+string(c))
	}
	return out
}
