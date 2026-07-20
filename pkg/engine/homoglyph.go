package engine

import "github.com/andoniaf/yatt/pkg/engine/data"

func init() {
	Register(Homoglyph{})
}

// Homoglyph generates the variants produced by substituting a character, or
// an adjacent pair of characters, for something that looks like it: a
// digit/letter look-alike ("0" for "o"), a script look-alike (Cyrillic "а"
// for Latin "a"), or a multi-character illusion ("rn" for "m", "vv" for
// "w"). The pair window is what lets both directions of a multi-character
// illusion fire from one table entry each.
//
// Substitution is applied twice — once over the seed's own label, and again
// over every variant that produced — so a compounded look-alike like a
// digit substitution combined with a script substitution is reachable in
// one technique, the same as dnstwist's two-pass homoglyph fuzzer.
type Homoglyph struct{}

// Name implements Technique.
func (Homoglyph) Name() string { return "homoglyph" }

// Permute generates without TLD context, using the unrestricted glyph set.
// engine.Permute never calls this directly — Homoglyph implements
// LabelBySuffix, so a real scan gates the Unicode component by the seed's
// suffix — but Permute must still exist to satisfy Technique, and this keeps
// the technique testable and usable on its own.
func (h Homoglyph) Permute(sld string) []string {
	return h.permute(sld, data.Merge(data.GlyphsASCII, data.GlyphsUnicode))
}

// PermuteWithSuffix implements LabelBySuffix.
//
// ASCII look-alikes never depend on the TLD — they need no IDN support to
// register — so they are always available. The Unicode component is
// dropped entirely for suffixes in data.IDNGatedTLDs, whose registries are
// known not to support the IDN registrations those candidates would
// require; every other suffix gets the full merged table.
func (Homoglyph) PermuteWithSuffix(sld, suffix string) []string {
	glyphs := data.GlyphsASCII
	if !data.IDNGatedTLDs[suffix] {
		glyphs = data.Merge(data.GlyphsASCII, data.GlyphsUnicode)
	}
	return Homoglyph{}.permute(sld, glyphs)
}

func (Homoglyph) permute(sld string, glyphs map[string][]string) []string {
	seen := map[string]bool{sld: true}
	first := mix(sld, glyphs, seen)

	out := make([]string, 0, len(first))
	out = append(out, first...)
	for _, variant := range first {
		out = append(out, mix(variant, glyphs, seen)...)
	}
	return out
}

// mix returns every one-substitution variant of s over a one- or
// two-character window, skipping anything already produced — by an earlier
// call recorded in seen, or a repeat within this one.
func mix(s string, glyphs map[string][]string, seen map[string]bool) []string {
	runes := []rune(s)
	var out []string
	for window := 1; window <= 2; window++ {
		for i := 0; i+window <= len(runes); i++ {
			key := string(runes[i : i+window])
			for _, g := range glyphs[key] {
				variant := string(runes[:i]) + g + string(runes[i+window:])
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
