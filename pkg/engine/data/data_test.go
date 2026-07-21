package data_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/andoniaf/yatt/pkg/engine/data"
)

// The tables in this package are regenerated from upstream snapshots, and a
// regeneration is expected to change them — new TLDs get delegated, and each
// Unicode release adds confusables. What these tests guard is not the exact
// contents but the properties a bad regeneration would break: the filter
// silently widening or narrowing, the list arriving truncated, or the
// well-known homographs the tool exists to catch going missing.
//
// This matters because the tables are otherwise their own fixture. An
// assertion written as `len(got) == len(data.IANATLDs)` compares the table to
// itself and passes for any contents.

// TestGlyphsUnicodeCoversKnownHomographs pins the substitutions every
// typosquatting tool is expected to catch. If a regeneration drops the Cyrillic
// а/е/о/р/с or the Greek ο, the filter has narrowed and homograph coverage went
// with it.
func TestGlyphsUnicodeCoversKnownHomographs(t *testing.T) {
	tests := []struct {
		ascii string
		want  string
		name  string
	}{
		{"a", "а", "Cyrillic small a"},
		{"e", "е", "Cyrillic small ie"},
		{"o", "о", "Cyrillic small o"},
		{"p", "р", "Cyrillic small er"},
		{"c", "с", "Cyrillic small es"},
		{"y", "у", "Cyrillic small u"},
		{"x", "х", "Cyrillic small ha"},
		{"o", "ο", "Greek small omicron"},
		{"i", "і", "Cyrillic small byelorussian-ukrainian i"},
		{"j", "ј", "Cyrillic small je"},
		{"s", "ѕ", "Cyrillic small dze"},
		{"h", "һ", "Cyrillic small shha"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !slices.Contains(data.GlyphsUnicode[tt.ascii], tt.want) {
				t.Errorf("GlyphsUnicode[%q] = %q, want it to contain %q (%s)",
					tt.ascii, data.GlyphsUnicode[tt.ascii], tt.want, tt.name)
			}
		})
	}
}

// TestGlyphsUnicodeStaysWithinItsBlocks is the other direction: the filter must
// not widen. Mathematical Alphanumeric Symbols and Enclosed Alphanumerics are
// confusable but not registrable, so their appearance means the block filter
// was dropped or loosened and every scan just got noisier.
func TestGlyphsUnicodeStaysWithinItsBlocks(t *testing.T) {
	blocks := []struct {
		name   string
		lo, hi rune
	}{
		{"Latin-1 Supplement", 0x0080, 0x00FF},
		{"Latin Extended-A", 0x0100, 0x017F},
		{"Latin Extended-B", 0x0180, 0x024F},
		{"IPA Extensions", 0x0250, 0x02AF},
		{"Greek and Coptic", 0x0370, 0x03FF},
		{"Cyrillic", 0x0400, 0x04FF},
		{"Cyrillic Supplement", 0x0500, 0x052F},
		{"Armenian", 0x0530, 0x058F},
		{"Halfwidth and Fullwidth Forms", 0xFF00, 0xFFEF},
	}
	inAnyBlock := func(r rune) bool {
		for _, b := range blocks {
			if r >= b.lo && r <= b.hi {
				return true
			}
		}
		return false
	}

	for ascii, glyphs := range data.GlyphsUnicode {
		for _, g := range glyphs {
			runes := []rune(g)
			if len(runes) != 1 {
				t.Errorf("GlyphsUnicode[%q] contains %q, want single codepoints only", ascii, g)
				continue
			}
			if !inAnyBlock(runes[0]) {
				t.Errorf("GlyphsUnicode[%q] contains %q (U+%04X), which is outside the accepted blocks",
					ascii, g, runes[0])
			}
		}
	}
}

// TestGlyphsUnicodeKeysAreASCIIAlnum guards the inversion: keys are the
// character being imitated, so anything but a plain letter or digit means the
// target filter broke.
func TestGlyphsUnicodeKeysAreASCIIAlnum(t *testing.T) {
	for ascii := range data.GlyphsUnicode {
		if len(ascii) != 1 {
			t.Errorf("GlyphsUnicode key %q is not a single character", ascii)
			continue
		}
		c := ascii[0]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			t.Errorf("GlyphsUnicode key %q is not a lowercase ASCII letter or digit", ascii)
		}
	}
}

// TestIANATLDsLooksLikeTheRootZone catches a truncated or mis-parsed download.
// The root zone has carried well over a thousand TLDs since the 2013 gTLD
// expansion, so a list of a few dozen means the fetch returned an error page.
func TestIANATLDsLooksLikeTheRootZone(t *testing.T) {
	if got := len(data.IANATLDs); got < 1000 {
		t.Errorf("IANATLDs has %d entries, want the full root zone (>1000)", got)
	}

	// A few delegations old enough to be safe anchors.
	for _, tld := range []string{"com", "net", "org", "io", "co", "uk", "de"} {
		if !data.IANATLDSet[tld] {
			t.Errorf("IANATLDs is missing %q", tld)
		}
	}
}

// TestIANATLDsAreNormalized guards the one transformation the generator does.
func TestIANATLDsAreNormalized(t *testing.T) {
	for _, tld := range data.IANATLDs {
		if tld != strings.ToLower(tld) {
			t.Errorf("IANATLDs contains %q, want it lowercased", tld)
		}
		if strings.ContainsAny(tld, " \t.") || tld == "" {
			t.Errorf("IANATLDs contains %q, want a bare lowercase label", tld)
		}
	}
}

// TestIANATLDSetMatchesTheSlice keeps the hand-maintained set in tldset.go
// honest against the generated slice it is derived from.
func TestIANATLDSetMatchesTheSlice(t *testing.T) {
	if got, want := len(data.IANATLDSet), len(data.IANATLDs); got != want {
		t.Errorf("IANATLDSet has %d entries, want %d — the slice has duplicates or the set is stale", got, want)
	}
	for _, tld := range data.IANATLDs {
		if !data.IANATLDSet[tld] {
			t.Errorf("IANATLDSet is missing %q", tld)
		}
	}
}
