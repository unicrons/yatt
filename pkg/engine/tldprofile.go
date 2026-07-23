package engine

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/unicrons/yatt/pkg/engine/data"
)

// TLD profile names. "common" is the default: fast, low-noise, and
// seed-aware. "full" sweeps the whole IANA list — deep coverage, at the
// cost of a scan that is much slower and far more likely to get itself
// rate-limited.
const (
	TLDProfileCommon = "common"
	TLDProfileFull   = "full"
)

// ResolveTLDProfile returns the TLD candidates for the named profile, given
// the seed's own suffix — which "common" needs for its near-miss set. An
// empty name resolves to "common", the default.
func ResolveTLDProfile(name, seedSuffix string) ([]string, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", TLDProfileCommon:
		return CommonTLDs(seedSuffix), nil
	case TLDProfileFull:
		return data.IANATLDs, nil
	default:
		return nil, fmt.Errorf("unknown TLD profile %q (available: %s, %s)", name, TLDProfileCommon, TLDProfileFull)
	}
}

// CommonTLDs returns the "common" TLD profile for a seed's own suffix:
// curated common TLDs, union curated abused TLDs, union near-misses of the
// seed's own suffix — each list's own order preserved, so the nearest,
// most-recognizable swaps (starting with the seed's own near-misses) lead
// the candidate list.
func CommonTLDs(seedSuffix string) []string {
	seen := make(map[string]bool)
	var out []string
	add := func(tlds []string) {
		for _, t := range tlds {
			if seen[t] {
				continue
			}
			seen[t] = true
			out = append(out, t)
		}
	}
	add(NearMisses(seedSuffix))
	add(data.CommonTLDs)
	add(data.AbusedTLDs)
	return out
}

// curatedNearMisses covers near-misses character-edit distance cannot
// reach: a multi-label suffix that is not itself a single IANA TLD, so it
// can never be found by editing seedSuffix and checking data.IANATLDSet.
// See PROVENANCE.md.
var curatedNearMisses = map[string][]string{
	"com": {"co.uk"},
	"net": {"ne.jp"},
	"org": {"or.jp"},
}

// NearMisses returns TLDs close to seedSuffix: single-character edits of
// seedSuffix that are themselves real, registrable TLDs (a near-miss must
// be a domain someone could actually register), plus the curated multi-label
// look-alikes character-edit distance cannot reach — .com's registrants
// being confused by .co.uk being the canonical example.
func NearMisses(seedSuffix string) []string {
	seen := make(map[string]bool)
	var out []string
	consider := func(candidate string) {
		if candidate == "" || candidate == seedSuffix || seen[candidate] {
			return
		}
		seen[candidate] = true
		out = append(out, candidate)
	}

	if !strings.Contains(seedSuffix, ".") {
		for _, v := range (Omission{}).Permute(seedSuffix) {
			if data.IANATLDSet[v] {
				consider(v)
			}
		}
		for _, v := range (Transposition{}).Permute(seedSuffix) {
			if data.IANATLDSet[v] {
				consider(v)
			}
		}
	}
	for _, extra := range curatedNearMisses[seedSuffix] {
		consider(extra)
	}
	return out
}

// ParseTLDList reads a custom TLD list, one per line: blank lines and lines
// starting with "#" are ignored, and a leading "." is stripped so both
// "com" and ".com" are accepted. This is the format --tld-file reads.
func ParseTLDList(r io.Reader) ([]string, error) {
	var out []string
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, normalizeSuffix(line))
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// ResolveTLDs rewrites every TLD-swap technique in techniques to swap
// against a specific TLD list, resolved from custom (highest precedence,
// typically --tld-file), falling back to the named profile.
//
// Techniques not selected are left untouched, and if TLD swap was not
// selected at all this does no work: resolving a TLD profile means reading
// either a curated list or the ~1,500-entry IANA list, which is wasted work
// for a scan that never asked for TLD swap.
func ResolveTLDs(techniques []Technique, seedSuffix, profile string, custom []string) ([]Technique, error) {
	out := make([]Technique, len(techniques))
	copy(out, techniques)

	for i, t := range out {
		if _, ok := t.(TLD); !ok {
			continue
		}
		tlds := custom
		if len(tlds) == 0 {
			resolved, err := ResolveTLDProfile(profile, seedSuffix)
			if err != nil {
				return nil, err
			}
			tlds = resolved
		}
		out[i] = TLD{TLDs: tlds}
	}
	return out, nil
}
