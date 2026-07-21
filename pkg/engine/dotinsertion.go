package engine

func init() {
	Register(DotInsertion{})
}

// DotInsertion generates the variants produced by inserting a dot inside the
// label, modelling a squatter who registers the tail of a brand name and
// serves the head as a subdomain: "unicrons.cloud" read as "uni.crons.cloud",
// where the name actually registered is "crons.cloud".
type DotInsertion struct{}

// Name implements Technique.
func (DotInsertion) Name() string { return "dot-insertion" }

// Permute is unused: dot-insertion moves the registrable boundary rather than
// editing the label, so it implements DomainSplit instead. It must still
// exist to satisfy Technique; a dotted SLD is not a label, so there is no
// SLD-only answer to give and it returns nil rather than guessing.
func (DotInsertion) Permute(string) []string { return nil }

// PermuteSplit implements DomainSplit. The dot goes at each interior position
// in turn, left to right — the same range dnstwist's subdomain fuzzer walks,
// so side-by-side comparisons line up — skipping positions that would leave a
// hyphen at a label edge.
func (DotInsertion) PermuteSplit(sld string) [][2]string {
	runes := []rune(sld)
	if len(runes) < 3 {
		return nil
	}

	out := make([][2]string, 0, len(runes)-2)
	seen := make(map[[2]string]bool, len(runes)-2)
	for i := 1; i < len(runes)-1; i++ {
		if runes[i] == '-' || runes[i-1] == '-' {
			continue
		}
		split := [2]string{string(runes[:i]), string(runes[i:])}
		if seen[split] {
			continue
		}
		seen[split] = true
		out = append(out, split)
	}
	return out
}
