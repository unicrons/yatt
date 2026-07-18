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

// Candidate is a single generated look-alike domain.
type Candidate struct {
	// Domain is the full candidate name, with the seed's subdomain reattached.
	Domain string
	// Registrable is the candidate's eTLD+1 — the name the NS query targets.
	Registrable string
	// SLD is the mutated second-level label.
	SLD string
	// Technique names the technique that first produced this candidate.
	Technique string
}

// TechniqueOriginal labels the seed's own row. It is not a registered technique
// — nothing generates it — but it travels through the same pipeline as the
// candidates so the seed is resolved, stored and diffed on one code path.
const TechniqueOriginal = "original"

// registry holds the known techniques in registration order, which is also the
// order Permute walks them in.
var registry []Technique

// Register adds a technique to the registry. It panics on a duplicate name,
// since that can only be a programming error.
func Register(t Technique) {
	for _, existing := range registry {
		if existing.Name() == t.Name() {
			panic(fmt.Sprintf("engine: technique %q registered twice", t.Name()))
		}
	}
	registry = append(registry, t)
}

// All returns every registered technique, in registration order.
func All() []Technique {
	out := make([]Technique, len(registry))
	copy(out, registry)
	return out
}

// Names returns the names of every registered technique, sorted.
func Names() []string {
	names := make([]string, 0, len(registry))
	for _, t := range registry {
		names = append(names, t.Name())
	}
	sort.Strings(names)
	return names
}

// Lookup returns the registered technique with the given name.
func Lookup(name string) (Technique, error) {
	for _, t := range registry {
		if t.Name() == name {
			return t, nil
		}
	}
	return nil, fmt.Errorf("unknown technique %q (available: %s)", name, strings.Join(Names(), ", "))
}

// Select resolves a list of technique names to their implementations,
// preserving registration order rather than the order the names were given, so
// the candidate list stays stable regardless of how flags were typed.
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
	for _, t := range registry {
		if wanted[t.Name()] {
			out = append(out, t)
		}
	}
	return out, nil
}

// Permute runs every technique over the seed's SLD and returns the deduplicated
// candidate set.
//
// Ordering is deterministic: techniques are applied in the order given, and
// each technique's own output order is preserved. A candidate produced by more
// than one technique is attributed to the first one that produced it, and the
// seed itself is never returned as a candidate.
func Permute(seed Seed, techniques []Technique) []Candidate {
	var candidates []Candidate
	seen := make(map[string]bool)

	for _, t := range techniques {
		for _, sld := range t.Permute(seed.SLD) {
			if sld == seed.SLD || !ValidLabel(sld) {
				continue
			}
			registrable := sld + "." + seed.Suffix
			if seen[registrable] {
				continue
			}
			seen[registrable] = true

			domain := registrable
			if seed.Subdomain != "" {
				domain = seed.Subdomain + "." + registrable
			}
			candidates = append(candidates, Candidate{
				Domain:      domain,
				Registrable: registrable,
				SLD:         sld,
				Technique:   t.Name(),
			})
		}
	}
	return candidates
}

// Original returns the seed's own row, shaped like a candidate.
func Original(seed Seed) Candidate {
	return Candidate{
		Domain:      seed.String(),
		Registrable: seed.Registrable(),
		SLD:         seed.SLD,
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
