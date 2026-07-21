package engine

import "strings"

func init() {
	Register(TLD{})
}

// TLD generates the variants produced by swapping the seed's own suffix for
// another one, modelling a squatter registering the same brand name under a
// different top-level domain.
//
// TLDs is the candidate list to swap against. A zero-value TLD{} — the one
// engine.All returns — uses CommonTLDs' seed-aware default, so `yatt scan`
// works out of the box; a scan honouring --tld-profile or --tld-file
// constructs its own TLD{TLDs: ...} and substitutes it into the technique
// list before calling Permute (see ResolveTLDs).
type TLD struct {
	TLDs []string
}

// TechniqueTLD is the TLD swap technique's registered name. It is exported
// because the scan pipeline treats this technique specially when capping:
// its candidate count is exactly the TLD list the user chose.
const TechniqueTLD = "tld"

// Name implements Technique.
func (TLD) Name() string { return TechniqueTLD }

// Permute is unused: TLD varies the suffix, not the label, so it implements
// SuffixSwap instead. It must still exist to satisfy Technique; there is no
// meaningful SLD-only answer to give without the seed's own suffix, so it
// returns nil rather than guessing.
func (TLD) Permute(string) []string { return nil }

// PermuteSuffixes implements SuffixSwap. suffix is the seed's own suffix,
// which conveniently is also everything CommonTLDs' seed-aware near-miss
// set needs.
func (t TLD) PermuteSuffixes(suffix string) []string {
	tlds := t.TLDs
	if tlds == nil {
		tlds = CommonTLDs(suffix)
	}

	seen := make(map[string]bool, len(tlds))
	out := make([]string, 0, len(tlds))
	for _, candidate := range tlds {
		candidate = normalizeSuffix(candidate)
		if candidate == "" || candidate == suffix || seen[candidate] {
			continue
		}
		seen[candidate] = true
		out = append(out, candidate)
	}
	return out
}

// normalizeSuffix puts a caller-supplied TLD in the form Permute compares
// against a seed's own suffix: lowercase, no leading or trailing dots.
func normalizeSuffix(s string) string {
	return strings.Trim(strings.ToLower(strings.TrimSpace(s)), ".")
}
