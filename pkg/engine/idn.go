package engine

import "golang.org/x/net/idna"

// candidateProfile converts candidate labels to the wire form a registry or
// browser would actually resolve.
//
// UTS 46 mapping (MapForLookup) is what makes the output canonical: it folds
// case and width before punycoding, so a confusable like Cyrillic "З"
// encodes to the same "xn--" form a browser would look up. Without it, the
// bare Punycode profile encodes "Зoom" as "xn--oom-b9c" while the registrable
// homograph is the lowercased "xn--oom-ydd" — a candidate that would resolve
// NXDOMAIN forever while the real look-alike stays invisible. The mapping
// also collapses fullwidth forms ("ｅxample") back to their ASCII originals,
// which addCandidate then drops as equal to the seed instead of querying
// unregistrable junk.
//
// CheckHyphens is disabled because the mapped profiles otherwise reject a
// hyphen in the label's third and fourth position unless the label already
// carries the "xn--" ACE prefix, which would silently drop real candidates
// shaped like "ab--cd": the same shape as legitimate CDN hostnames (e.g.
// "r3---sn-apo3qvuoxuxbt-j5pe") that browsers resolve every day without
// complaint.
var candidateProfile = idna.New(idna.MapForLookup(), idna.CheckHyphens(false))

// ToASCII converts a label to the ASCII wire form DNS resolves, converting
// non-ASCII labels to their canonical punycode ("xn--") form via
// candidateProfile.
//
// ok is false when label cannot be represented as a registrable domain label
// — malformed UTF-8, punycode overflow, or a rune IDNA2008 disallows
// outright (so nobody could register it); the candidate it belongs to is
// dropped rather than resolved, since there is nothing a DNS query could
// meaningfully ask for.
func ToASCII(label string) (ascii string, ok bool) {
	out, err := candidateProfile.ToASCII(label)
	if err != nil {
		return "", false
	}
	return out, true
}

// ToUnicode converts a domain from its punycode ("xn--") wire form back to the
// Unicode form a browser's address bar would display. A domain that cannot be
// decoded is returned unchanged: the ASCII form is always a valid, if less
// readable, way to show it.
func ToUnicode(domain string) string {
	out, err := idna.ToUnicode(domain)
	if err != nil {
		return domain
	}
	return out
}
