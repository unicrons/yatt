// Code generated from Unicode confusables.txt; DO NOT EDIT BY HAND.
// Source:  https://www.unicode.org/Public/security/latest/confusables.txt
// Version: 17.0.0 (dated 2025-07-22, 05:49:37 GMT)
// Regenerate with: go generate ./pkg/engine/data
// See PROVENANCE.md for the filter this applies and the reasoning behind it.

package data

// GlyphsUnicode maps an ASCII letter or digit to the Unicode characters
// visually confusable with it, restricted to scripts plausibly used in
// homograph attacks (Latin-1 Supplement, Latin Extended-A/B, IPA Extensions,
// Greek and Coptic, Cyrillic (+ Supplement), Armenian, and the Halfwidth and
// Fullwidth Forms block). See PROVENANCE.md.
var GlyphsUnicode = map[string][]string{
	"2": {"Ƨ", "Ϩ"},
	"3": {"Ʒ", "Ȝ", "З", "Ӡ"},
	"5": {"Ƽ"},
	"6": {"Ϭ", "б"},
	"8": {"Ȣ", "ȣ"},
	"a": {"ɑ", "α", "а", "ａ"},
	"b": {"Ƅ", "Ь"},
	"c": {"ϲ", "с", "ｃ"},
	"d": {"ԁ"},
	"e": {"е", "ҽ", "ｅ"},
	"f": {"ſ", "ƒ", "ք"},
	"g": {"ƍ", "ɡ", "ց", "ｇ"},
	"h": {"һ", "հ", "ｈ"},
	"i": {"ı", "ɩ", "ɪ", "ͺ", "ι", "і", "ւ", "ｉ"},
	"j": {"ϳ", "ј", "ｊ"},
	"l": {"Ɩ", "ǀ", "Ι", "І", "Ӏ", "ӏ", "Ｉ", "ｌ", "￨"},
	"n": {"ո", "ռ"},
	"o": {"ο", "σ", "ϭ", "о", "օ", "ｏ"},
	"p": {"þ", "ƿ", "ρ", "ϱ", "ϸ", "р", "ｐ"},
	"q": {"ԛ", "գ", "զ"},
	"r": {"г"},
	"s": {"ƽ", "ѕ", "ｓ"},
	"u": {"ʋ", "υ", "ս"},
	"v": {"ν", "ѵ", "ｖ"},
	"w": {"ɯ", "ш", "ѡ", "ԝ", "ա"},
	"x": {"×", "х", "ｘ"},
	"y": {"ɣ", "ʏ", "γ", "у", "ү", "ｙ"},
}
