package data

// GlyphsASCII maps a one- or two-character DNS-legal ASCII window to the
// windows that look like it: digit/letter look-alikes ("0"/"o", "1"/"l")
// and the classic multi-character illusions ("rn" reads as "m", "vv" reads
// as "w"). Homoglyph substitution looks up both window sizes, which is what
// lets a single table produce "m" -> "rn" and "rn" -> "m" from one entry
// each, in either direction.
//
// Unlike GlyphsUnicode this table needs no IDN registration to be
// registerable — every value is plain ASCII — so it is never gated by TLD.
// See PROVENANCE.md.
var GlyphsASCII = map[string][]string{
	"0":  {"o"},
	"o":  {"0"},
	"1":  {"l", "i"},
	"l":  {"1"},
	"i":  {"1"},
	"2":  {"z"},
	"z":  {"2"},
	"3":  {"e"},
	"e":  {"3"},
	"4":  {"a"},
	"a":  {"4"},
	"5":  {"s"},
	"s":  {"5"},
	"6":  {"b"},
	"7":  {"t"},
	"t":  {"7"},
	"8":  {"b"},
	"b":  {"8", "6"},
	"9":  {"g"},
	"g":  {"9"},
	"u":  {"v"},
	"v":  {"u"},
	"m":  {"rn", "nn"},
	"rn": {"m"},
	"nn": {"m"},
	"w":  {"vv"},
	"vv": {"w"},
	"d":  {"cl"},
	"cl": {"d"},
}
