// Package triage defines an analyst's verdict on a candidate domain.
//
// A verdict is keyed by seed and candidate rather than by scan, so it outlives
// every individual run. That is the whole point: roughly nineteen in twenty
// look-alike hits are parked or defensive registrations, and a tool that makes
// an analyst re-decide them on every scan is a tool that gets abandoned after
// the second scan.
package triage

import (
	"fmt"
	"strings"
)

// Status is the verdict recorded against a candidate.
type Status string

const (
	// StatusNew is the implicit state of a candidate nobody has judged yet. It
	// is never stored: a candidate is "new" precisely because it has no record.
	StatusNew Status = "new"
	// StatusBenign marks a candidate judged harmless.
	StatusBenign Status = "benign"
	// StatusSuspicious marks a candidate worth watching but not yet proven bad.
	StatusSuspicious Status = "suspicious"
	// StatusMalicious marks a confirmed abusive look-alike.
	StatusMalicious Status = "malicious"
	// StatusWatchlist marks a candidate to keep surfacing regardless of verdict.
	StatusWatchlist Status = "watchlist"
	// StatusIgnored marks a candidate to stop surfacing.
	StatusIgnored Status = "ignored"
	// StatusFalsePositive marks a candidate the engine should not have produced.
	StatusFalsePositive Status = "false_positive"
	// StatusOwned marks a domain the organisation registered itself, defensively
	// or otherwise. Distinguishing these from third-party registrations is the
	// difference between a report an analyst reads and one they filter.
	StatusOwned Status = "owned"
)

// Statuses lists every valid status, ordered roughly by escalation so help text
// and error messages read sensibly rather than alphabetically.
var Statuses = []Status{
	StatusNew,
	StatusBenign,
	StatusSuspicious,
	StatusMalicious,
	StatusWatchlist,
	StatusIgnored,
	StatusFalsePositive,
	StatusOwned,
}

// String implements fmt.Stringer.
func (s Status) String() string { return string(s) }

// Valid reports whether s is one of the known statuses.
func (s Status) Valid() bool {
	for _, known := range Statuses {
		if s == known {
			return true
		}
	}
	return false
}

// Triaged reports whether a human has actually judged this candidate. An unset
// or "new" status has not been judged, so both answer false.
func (s Status) Triaged() bool {
	return s != "" && s != StatusNew
}

// Parse resolves user input to a Status.
//
// Input is normalised before matching: case is folded and hyphens are accepted
// in place of underscores, so `--status False-Positive` works and only a
// genuinely unknown value is rejected. The error names every valid status,
// because a rejected verdict with no list of alternatives just sends the user
// to `--help`.
func Parse(s string) (Status, error) {
	normalized := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(s)), "-", "_")
	if normalized == "" {
		return "", fmt.Errorf("empty triage status (valid: %s)", List())
	}
	if status := Status(normalized); status.Valid() {
		return status, nil
	}
	return "", fmt.Errorf("unknown triage status %q (valid: %s)", s, List())
}

// ParseAll resolves a list of user-supplied statuses, rejecting the whole list
// if any single entry is unknown. Partially honouring a filter would silently
// widen it, which for a triage filter means showing candidates the analyst
// asked to hide.
func ParseAll(values []string) ([]Status, error) {
	var out []Status
	for _, value := range values {
		// A single flag may carry a comma-separated list, so both
		// `--status a,b` and `--status a --status b` mean the same thing.
		for _, part := range strings.Split(value, ",") {
			if strings.TrimSpace(part) == "" {
				continue
			}
			status, err := Parse(part)
			if err != nil {
				return nil, err
			}
			out = append(out, status)
		}
	}
	return out, nil
}

// List returns every valid status as a comma-separated string, for help text
// and error messages. It includes "new", which is a legitimate thing to filter
// a report by even though it is not a verdict anyone records.
func List() string {
	return join(Statuses)
}

// ListSettable returns every status a verdict may be set to — everything except
// "new". Error messages about recording a verdict use this rather than List, so
// they never offer the one value they are in the middle of rejecting.
func ListSettable() string {
	settable := make([]Status, 0, len(Statuses)-1)
	for _, s := range Statuses {
		if s != StatusNew {
			settable = append(settable, s)
		}
	}
	return join(settable)
}

func join(statuses []Status) string {
	names := make([]string, 0, len(statuses))
	for _, s := range statuses {
		names = append(names, string(s))
	}
	return strings.Join(names, ", ")
}

// CanTransition reports whether a candidate may move from one status to
// another, returning an explanatory error when it may not.
//
// Analysts revise verdicts constantly — a suspicious candidate turns out benign,
// a benign one starts serving a phishing page — so every judged-to-judged move
// is allowed, including a move to the same status, which is how a note is
// amended without changing the verdict.
//
// The one rule is that nothing may transition *to* "new". "New" means "no one
// has looked at this yet", and once someone has, that is no longer true; a
// record exists and its history is the point. An analyst wanting to undo a
// mistaken verdict sets the correct one instead.
func CanTransition(from, to Status) error {
	if from != "" && !from.Valid() {
		return fmt.Errorf("unknown current triage status %q", from)
	}
	if !to.Valid() {
		return fmt.Errorf("unknown triage status %q (valid: %s)", to, List())
	}
	if to == StatusNew {
		return fmt.Errorf("cannot set a candidate back to %q: it records that nobody has judged the candidate yet; set the correct verdict instead (valid: %s)",
			StatusNew, ListSettable())
	}
	return nil
}
