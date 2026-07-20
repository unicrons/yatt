package engine

import "golang.org/x/net/idna"

// ToASCII converts a label to the ASCII wire form DNS resolves, converting
// non-ASCII labels to their punycode ("xn--") form.
//
// It uses idna.ToASCII — the Punycode profile, which does minimal
// validation — rather than the stricter Lookup or Registration profiles.
// Those reject a hyphen in the label's third and fourth position unless the
// label already carries the "xn--" ACE prefix, which would silently drop
// real candidates shaped like "ab--cd": the same shape as legitimate CDN
// hostnames (e.g. "r3---sn-apo3qvuoxuxbt-j5pe") that browsers resolve every
// day without complaint. Rejecting only structurally unencodable input, and
// letting DNS itself be the arbiter of whether an ASCII-safe label is
// registered, is the conservative choice at candidate-generation time: it
// would rather resolve one label too many than silently drop one a
// squatter could actually register.
//
// ok is false only when label cannot be represented as a valid domain label
// at all (malformed UTF-8, punycode overflow); the candidate it belongs to
// is dropped rather than resolved, since there is nothing a DNS query could
// meaningfully ask for.
func ToASCII(label string) (ascii string, ok bool) {
	out, err := idna.ToASCII(label)
	if err != nil {
		return "", false
	}
	return out, true
}
