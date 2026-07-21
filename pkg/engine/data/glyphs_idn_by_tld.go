package data

// IDNGatedTLDs lists suffixes whose registries are known not to accept the
// broad Unicode registrations a homoglyph candidate would need — either
// because the ccTLD restricts registrations to a single script that is not
// Latin (so a Latin-confusable candidate could never be registered there),
// or because the registry's public IDN policy does not (yet) offer IDN
// registration at all. For these suffixes the homoglyph technique emits
// only its ASCII substitutions and drops the Unicode component entirely,
// mirroring dnstwist's per-TLD glyph gating concept (see PROVENANCE.md) —
// without it, most Unicode homoglyph candidates under these TLDs are
// unregistrable noise a scan would resolve for nothing.
//
// This is a curated, conservative starting set, not an exhaustive audit of
// every registry's current IDN policy — policies change, and a candidate
// that is IDNA-valid but not actually registerable under a ungated TLD is
// still filtered out at resolution time by simply never registering (NS
// Rcode NXDOMAIN), so an incomplete list here costs extra queries, not
// wrong results.
var IDNGatedTLDs = map[string]bool{
	"us":  true,
	"jp":  true,
	"cn":  true,
	"kr":  true,
	"tw":  true,
	"sa":  true,
	"il":  true,
	"gov": true,
	"mil": true,
}
