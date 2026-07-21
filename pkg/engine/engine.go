// Package engine generates look-alike domain candidates from a seed domain.
//
// Each technique is a pure string -> set function over the seed's second-level
// label, so techniques are independently testable and trivially reusable
// outside the CLI.
package engine

import (
	"fmt"
	"sort"
	"strings"
)

// Technique produces look-alike variants of a single second-level label.
//
// Implementations must be deterministic: the same input must always yield the
// same slice in the same order, because scan-to-scan diffing depends on a
// stable candidate list.
type Technique interface {
	// Name is the stable identifier used by flags, config and output.
	Name() string
	// Permute returns variants of sld. The input itself may be returned; the
	// orchestrator filters it out.
	Permute(sld string) []string
}

// LabelBySuffix is implemented by a technique whose label variants also
// depend on the seed's own suffix — currently only homoglyph, which gates
// its Unicode substitutions by the seed's TLD (data.IDNGatedTLDs). Permute
// still runs when the technique is used outside a seed's context (for
// example, in a technique-level test); Permute must still exist to satisfy
// Technique, but Permute is preferred over it by Permute below whenever a
// seed's suffix is available.
type LabelBySuffix interface {
	PermuteWithSuffix(sld, suffix string) []string
}

// SuffixSwap is implemented by a technique that varies the candidate's
// suffix instead of its label — currently only TLD swap, whose candidates
// keep the seed's own SLD and swap only what follows it.
type SuffixSwap interface {
	// PermuteSuffixes returns alternate suffixes for suffix. The input
	// itself may be returned; Permute filters it out.
	PermuteSuffixes(suffix string) []string
}

// Candidate is a single generated look-alike domain.
type Candidate struct {
	// Domain is the full candidate name, with the seed's subdomain reattached.
	Domain string
	// Registrable is the candidate's eTLD+1 — the name the NS query targets.
	Registrable string
	// SLD is the mutated second-level label.
	SLD string
	// Suffix is the candidate's public suffix. It is carried explicitly rather
	// than re-derived from Registrable because it is the zone wildcard detection
	// probes, and a scan that swaps TLDs spreads its candidates across many of
	// them.
	Suffix string
	// Technique names the technique that first produced this candidate.
	Technique string
}

// TechniqueOriginal labels the seed's own row. It is not a registered technique
// — nothing generates it — but it travels through the same pipeline as the
// candidates so the seed is resolved, stored and diffed on one code path.
const TechniqueOriginal = "original"

// registry holds every known technique, keyed by name.
var registry = map[string]Technique{}

// canonicalOrder is the nearest-first application order every multi-technique
// operation uses: single-character edits before the two techniques that fan
// out much wider (TLD swap sweeps a whole TLD list; homoglyph compounds two
// substitution passes). This is what makes cap.go's per-technique truncation
// keep the closest look-alikes first, and it is independent of registration
// order — which Go source file happens to register a technique in its
// init() must not be able to reorder a scan's output.
var canonicalOrder = []string{"omission", "transposition", "keyboard", "tld", "homoglyph"}

// Register adds a technique to the registry. It panics on a duplicate name,
// since that can only be a programming error.
func Register(t Technique) {
	if _, exists := registry[t.Name()]; exists {
		panic(fmt.Sprintf("engine: technique %q registered twice", t.Name()))
	}
	registry[t.Name()] = t
}

// orderedNames returns every registered technique name in canonicalOrder,
// followed alphabetically by any registered technique canonicalOrder does not
// mention — so a technique added without updating that list still appears
// deterministically instead of vanishing.
func orderedNames() []string {
	seen := make(map[string]bool, len(canonicalOrder))
	out := make([]string, 0, len(registry))
	for _, name := range canonicalOrder {
		if _, ok := registry[name]; ok {
			out = append(out, name)
			seen[name] = true
		}
	}
	var rest []string
	for name := range registry {
		if !seen[name] {
			rest = append(rest, name)
		}
	}
	sort.Strings(rest)
	return append(out, rest...)
}

// All returns every registered technique, in canonical (nearest-first) order.
func All() []Technique {
	names := orderedNames()
	out := make([]Technique, 0, len(names))
	for _, name := range names {
		out = append(out, registry[name])
	}
	return out
}

// Names returns the names of every registered technique, sorted.
func Names() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Lookup returns the registered technique with the given name.
func Lookup(name string) (Technique, error) {
	if t, ok := registry[name]; ok {
		return t, nil
	}
	return nil, fmt.Errorf("unknown technique %q (available: %s)", name, strings.Join(Names(), ", "))
}

// Select resolves a list of technique names to their implementations, in
// canonical order rather than the order the names were given, so the
// candidate list stays stable regardless of how flags were typed.
func Select(names []string) ([]Technique, error) {
	wanted := make(map[string]bool, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, err := Lookup(name); err != nil {
			return nil, err
		}
		wanted[name] = true
	}
	if len(wanted) == 0 {
		return nil, fmt.Errorf("no techniques selected")
	}

	var out []Technique
	for _, name := range orderedNames() {
		if wanted[name] {
			out = append(out, registry[name])
		}
	}
	return out, nil
}

// Permute runs every technique over the seed and returns the deduplicated
// candidate set.
//
// Ordering is deterministic: techniques are applied in the order given, and
// each technique's own output order is preserved. A candidate produced by more
// than one technique is attributed to the first one that produced it, and the
// seed itself is never returned as a candidate.
//
// Most techniques only vary the SLD label, keeping the seed's own suffix; a
// technique implementing LabelBySuffix additionally reads the seed's suffix
// (homoglyph, gating its Unicode substitutions by TLD); a technique
// implementing SuffixSwap varies the suffix instead, keeping the seed's own
// SLD (TLD swap). Callers select techniques by name via Select, so which of
// these a caller gets is a property of the technique, not something the
// caller chooses.
func Permute(seed Seed, techniques []Technique) []Candidate {
	var candidates []Candidate
	seen := make(map[string]bool)

	for _, t := range techniques {
		switch tech := t.(type) {
		case SuffixSwap:
			for _, suffix := range tech.PermuteSuffixes(seed.Suffix) {
				addCandidate(seed, t, seed.SLD, suffix, seen, &candidates)
			}
		case LabelBySuffix:
			for _, sld := range tech.PermuteWithSuffix(seed.SLD, seed.Suffix) {
				addCandidate(seed, t, sld, seed.Suffix, seen, &candidates)
			}
		default:
			for _, sld := range t.Permute(seed.SLD) {
				addCandidate(seed, t, sld, seed.Suffix, seen, &candidates)
			}
		}
	}
	return candidates
}

// addCandidate validates and appends one (sld, suffix) pair to candidates,
// deduping on the resulting registrable domain and converting the label to
// its DNS wire form.
//
// The wire-form conversion happens here rather than in each technique so
// every technique — SLD-mutating or suffix-mutating — gets it for free:
// today only homoglyph produces non-ASCII labels, but converting
// unconditionally means a future technique cannot forget it.
func addCandidate(seed Seed, t Technique, sld, suffix string, seen map[string]bool, candidates *[]Candidate) {
	if !ValidLabel(sld) || !validSuffix(suffix) {
		return
	}
	ascii, ok := ToASCII(sld)
	if !ok {
		return
	}
	suffix, ok = toASCIIDomain(suffix)
	if !ok {
		return
	}
	if ascii == seed.SLD && suffix == seed.Suffix {
		// Equal to the seed itself: WithOriginal already guards against this
		// independently, but refusing it here too means a stray duplicate
		// never even reaches the dedup map.
		return
	}

	registrable := ascii + "." + suffix
	if seen[registrable] {
		return
	}
	seen[registrable] = true

	domain := registrable
	if seed.Subdomain != "" {
		domain = seed.Subdomain + "." + registrable
	}
	*candidates = append(*candidates, Candidate{
		Domain:      domain,
		Registrable: registrable,
		SLD:         ascii,
		Suffix:      suffix,
		Technique:   t.Name(),
	})
}

// Original returns the seed's own row, shaped like a candidate.
//
// Every part is converted to its DNS wire form, exactly as addCandidate does
// for generated candidates: an IDN seed like "münchen.de" must be queried
// (and stored) as "xn--mnchen-3ya.de" — the resolver sends names verbatim,
// and raw UTF-8 on the wire answers NXDOMAIN, which would record the user's
// own domain as unregistered. A part that cannot be converted is kept as
// given: the seed's row must exist even when the query for it can only fail.
func Original(seed Seed) Candidate {
	sld := seed.SLD
	if ascii, ok := ToASCII(seed.SLD); ok {
		sld = ascii
	}
	suffix := seed.Suffix
	if ascii, ok := toASCIIDomain(seed.Suffix); ok {
		suffix = ascii
	}
	subdomain := seed.Subdomain
	if ascii, ok := toASCIIDomain(seed.Subdomain); ok {
		subdomain = ascii
	}

	registrable := sld + "." + suffix
	domain := registrable
	if subdomain != "" {
		domain = subdomain + "." + registrable
	}
	return Candidate{
		Domain:      domain,
		Registrable: registrable,
		SLD:         sld,
		Suffix:      suffix,
		Technique:   TechniqueOriginal,
	}
}

// WithOriginal prepends the seed's own row to a candidate set.
//
// The seed leads the report because it is the baseline the candidates are read
// against: whether a look-alike has mail configured is only interesting next to
// whether the real domain does.
//
// A candidate equal to the seed is dropped rather than emitted twice. Permute
// already refuses to return the seed's own SLD, so this is belt-and-braces
// against a technique that mutates the suffix instead of the label — the TLD
// swap can in principle land back on the original name.
func WithOriginal(seed Seed, candidates []Candidate) []Candidate {
	original := Original(seed)
	out := make([]Candidate, 0, len(candidates)+1)
	out = append(out, original)
	for _, c := range candidates {
		if c.Domain == original.Domain {
			continue
		}
		out = append(out, c)
	}
	return out
}

// ValidLabel reports whether s can be a single DNS label.
//
// Non-ASCII runes are accepted here because homoglyph candidates are only
// converted to their punycode form later; what this rejects is structurally
// impossible labels — empty, over-long, dotted, or hyphen-edged.
func ValidLabel(s string) bool {
	if s == "" || len(s) > 63 {
		return false
	}
	if strings.HasPrefix(s, "-") || strings.HasSuffix(s, "-") {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
		case r > 127:
		default:
			return false
		}
	}
	return true
}

// toASCIIDomain converts every dot-separated label of name to its DNS wire
// form. It exists because ToASCII operates on a single label, while suffixes
// and subdomains may carry dots ("co.uk", "a.b"). An empty name converts to
// itself, so an absent subdomain stays absent.
func toASCIIDomain(name string) (string, bool) {
	if name == "" {
		return "", true
	}
	labels := strings.Split(name, ".")
	for i, label := range labels {
		ascii, ok := ToASCII(label)
		if !ok {
			return "", false
		}
		labels[i] = ascii
	}
	return strings.Join(labels, "."), true
}

// validSuffix reports whether s can be a public suffix: one or more
// DNS-legal labels separated by dots, such as "com" or "co.uk". It exists
// separately from ValidLabel because a suffix, unlike an SLD, is allowed to
// contain dots.
func validSuffix(s string) bool {
	if s == "" {
		return false
	}
	for _, label := range strings.Split(s, ".") {
		if !ValidLabel(label) {
			return false
		}
	}
	return true
}
