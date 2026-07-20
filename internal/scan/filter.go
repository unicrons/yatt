package scan

import (
	"sort"

	"github.com/andoniaf/yatt/internal/triage"
)

// TriageFilter selects which findings to report by their standing verdict.
//
// Two directions are offered because analysts want both and neither expresses
// the other comfortably: Include answers "show me only what I flagged
// malicious", Exclude answers "show me everything except the domains we own" —
// and spelling the second as an Include list means restating the whole enum
// minus one entry every time.
type TriageFilter struct {
	// Include, when non-empty, keeps only findings whose verdict is listed.
	Include []triage.Status
	// Exclude drops findings whose verdict is listed. It is applied after
	// Include, so an explicitly excluded status stays hidden even if it was also
	// included.
	Exclude []triage.Status
}

// Empty reports whether the filter would keep everything.
func (f TriageFilter) Empty() bool {
	return len(f.Include) == 0 && len(f.Exclude) == 0
}

// Match reports whether a verdict passes the filter.
func (f TriageFilter) Match(status triage.Status) bool {
	// A finding from a run that read no verdicts has no status to judge; treat
	// it as unjudged rather than silently dropping it.
	if status == "" {
		status = triage.StatusNew
	}
	if len(f.Include) > 0 && !contains(f.Include, status) {
		return false
	}
	return !contains(f.Exclude, status)
}

// Apply returns the findings that pass the filter, preserving their order.
//
// The seed's own row is exempt. It is the baseline every candidate is read
// against, so a report that dropped it would answer "is this look-alike's mail
// setup like the real domain's?" with nothing to compare to. The same exemption
// should apply to the global --limit once capping lands: a cap is a bound on how
// many candidates to show, not a reason to lose the row explaining them.
func (f TriageFilter) Apply(findings []Finding) []Finding {
	if f.Empty() {
		return findings
	}
	out := make([]Finding, 0, len(findings))
	for _, finding := range findings {
		if finding.IsOriginal() || f.Match(finding.Triage) {
			out = append(out, finding)
		}
	}
	return out
}

// HideUnregistered drops candidates nobody has registered, preserving the
// order of those it keeps.
//
// This is the report's default because an unregistered look-alike is the
// overwhelming majority of any candidate set — a `--tld-profile full` scan
// generates hundreds of names that have never existed — and none of them is
// actionable. What an analyst is looking for is the handful somebody actually
// took.
//
// Two kinds of row survive regardless of their registered signal:
//
//   - The seed's own row, on the same reasoning that exempts it from the triage
//     filter: it is the baseline the candidates are read against.
//   - Any candidate whose resolution failed. Registered is false there because
//     nothing answered, not because the domain is provably unregistered, and
//     silently dropping those would turn a partially-failed scan into a
//     confidently short one.
func HideUnregistered(findings []Finding) []Finding {
	out := make([]Finding, 0, len(findings))
	for _, finding := range findings {
		if finding.IsOriginal() || finding.Registered || finding.Error != "" {
			out = append(out, finding)
		}
	}
	return out
}

// RegisteredFirst reorders a report so registered candidates lead it, and
// returns a new slice rather than shuffling the caller's.
//
// The sort is stable and the seed is pinned to the front, so this reorders the
// report without disturbing the two properties the rest of the tool rests on:
// candidates keep the permutation engine's nearest-first order within each
// group, and the seed stays the first row in every format. It is applied to the
// reported slice only, never to what was persisted, so the stored scan — and
// therefore the next run's diff — is unaffected by how this run was displayed.
func RegisteredFirst(findings []Finding) []Finding {
	out := make([]Finding, len(findings))
	copy(out, findings)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].IsOriginal() != out[j].IsOriginal() {
			return out[i].IsOriginal()
		}
		return out[i].Registered && !out[j].Registered
	})
	return out
}

func contains(statuses []triage.Status, status triage.Status) bool {
	for _, s := range statuses {
		if s == status {
			return true
		}
	}
	return false
}
