package scan

import "github.com/unicrons/yatt/internal/store"

// DiffStatus is a candidate's standing relative to the previous scan of the
// same seed.
type DiffStatus string

const (
	// DiffNew marks a candidate that the previous scan did not contain.
	DiffNew DiffStatus = "new"
	// DiffChanged marks a candidate whose boolean signals moved.
	DiffChanged DiffStatus = "changed"
	// DiffUnchanged marks a candidate whose signals are identical.
	DiffUnchanged DiffStatus = "unchanged"
	// DiffGone marks a candidate the previous scan contained and this one does
	// not. Gone findings are carried over from the previous scan, since there
	// is no current finding to attach the status to.
	DiffGone DiffStatus = "gone"
)

// DiffResult is the comparison of a scan against its predecessor.
type DiffResult struct {
	// Findings are the current scan's findings, each carrying a Diff status.
	Findings []Finding
	// Gone are the previous scan's findings that this scan did not produce,
	// each with Diff set to DiffGone.
	Gone []Finding
}

// signals is the tuple that defines whether a candidate "changed".
//
// Only the booleans participate. Addresses, nameservers and mail exchangers
// churn constantly on CDN- and cloud-hosted domains, and reporting every such
// rotation as a change would bury the transitions an analyst actually cares
// about: a candidate becoming registered, gaining MX, or gaining delegation.
//
// Wildcard is one of the booleans precisely because the others can sit
// still while it flips: a candidate leaving a catch-all zone for real
// infrastructure keeps registered/hasA and changes the only thing that
// matters — someone actually took the name.
type signals struct {
	registered bool
	hasNS      bool
	hasA       bool
	hasMX      bool
	wildcard   bool
}

func signalsOf(f Finding) signals {
	return signals{registered: f.Registered, hasNS: f.HasNS, hasA: f.HasA, hasMX: f.HasMX, wildcard: f.Wildcard}
}

// Diff compares the current findings against the previous scan's.
//
// Comparison is keyed on the candidate name, never on position, so a change in
// candidate ordering — from a new technique, a raised cap, or concurrent
// resolution — cannot masquerade as a set of changed findings.
func Diff(prior, current []Finding) DiffResult {
	if len(prior) == 0 {
		// With nothing to compare against, every candidate is new. Marking a
		// first scan "unchanged" would be worse than useless: the run that
		// establishes the baseline would look like the one run with no news.
		out := make([]Finding, len(current))
		copy(out, current)
		for i := range out {
			out[i].Diff = DiffNew
		}
		return DiffResult{Findings: out}
	}

	priorByCandidate := make(map[string]Finding, len(prior))
	for _, f := range prior {
		priorByCandidate[f.Candidate] = f
	}

	out := make([]Finding, len(current))
	copy(out, current)
	seen := make(map[string]bool, len(current))
	for i := range out {
		seen[out[i].Candidate] = true
		previous, ok := priorByCandidate[out[i].Candidate]
		switch {
		case !ok:
			out[i].Diff = DiffNew
		case out[i].Error != "":
			// A failed lookup stores its zero-valued signals alongside the
			// error, and zeroes compared as answers would report a phantom
			// lapse. The row still shows its error, but a lookup that failed
			// is not evidence that anything moved.
			out[i].Diff = DiffUnchanged
		case previous.Error != "":
			// The previous lookup failed, so this is the first clean
			// observation since. A positive signal here is news — a
			// registration first seen right after a transient failure must
			// not be absorbed as "unchanged", or it never appears in any
			// diff. A clean all-zeroes answer matches what the errored row's
			// zeroes implied, so only that direction stays quiet.
			if signalsOf(out[i]) != (signals{}) {
				out[i].Diff = DiffChanged
			} else {
				out[i].Diff = DiffUnchanged
			}
		case signalsOf(previous) != signalsOf(out[i]):
			out[i].Diff = DiffChanged
		default:
			out[i].Diff = DiffUnchanged
		}
	}

	// Walk prior in its own order rather than the map, so gone findings come
	// out deterministically.
	var gone []Finding
	for _, f := range prior {
		if seen[f.Candidate] {
			continue
		}
		f.Diff = DiffGone
		gone = append(gone, f)
	}

	return DiffResult{Findings: out, Gone: gone}
}

// Changes returns only the findings that differ from the previous scan, in
// report order: the seed's own row, then new, then changed, then gone.
//
// The seed leads whether or not it moved, because it is the baseline the changes
// are read against — a candidate that just gained MX means one thing when the
// real domain has MX and another when it does not. It is skipped in the three
// groups below so it is never listed twice.
func Changes(result DiffResult) []Finding {
	var changes []Finding
	for _, f := range result.Findings {
		if f.IsOriginal() {
			changes = append(changes, f)
			break
		}
	}
	for _, f := range result.Findings {
		if f.Diff == DiffNew && !f.IsOriginal() {
			changes = append(changes, f)
		}
	}
	for _, f := range result.Findings {
		if f.Diff == DiffChanged && !f.IsOriginal() {
			changes = append(changes, f)
		}
	}
	for _, f := range result.Gone {
		if !f.IsOriginal() {
			changes = append(changes, f)
		}
	}
	return changes
}

// HasChanges reports whether anything actually moved between the two scans.
//
// Changes always emits the seed's row, so its length can no longer answer this:
// a report of exactly one row means "nothing changed", not "one thing changed".
// A movement in the seed's own signals does count — a change to the domain being
// protected is news.
func HasChanges(result DiffResult) bool {
	for _, f := range result.Findings {
		if f.Diff == DiffNew || f.Diff == DiffChanged {
			return true
		}
	}
	return len(result.Gone) > 0
}

// fromStore maps persisted findings back into the domain type.
//
// The mapping lives here rather than in store because store must not import
// scan: scan persists through store, so the dependency only runs one way.
func fromStore(findings []store.Finding) []Finding {
	if len(findings) == 0 {
		return nil
	}
	out := make([]Finding, 0, len(findings))
	for _, f := range findings {
		out = append(out, Finding{
			Candidate:   f.Candidate,
			Registrable: f.Registrable,
			Technique:   f.Technique,
			Registered:  f.Registered,
			HasNS:       f.HasNS,
			HasA:        f.HasA,
			HasMX:       f.HasMX,
			Wildcard:    f.Wildcard,
			Addresses:   f.Addresses,
			NS:          f.NS,
			MX:          f.MX,
			Rcode:       f.Rcode,
			Error:       f.Error,
		})
	}
	return out
}

// toStore maps domain findings into their persisted form. The diff status is
// deliberately not persisted: it is a statement about a pair of scans, and is
// recomputed from the stored signals whenever it is needed.
func toStore(findings []Finding) []store.Finding {
	if len(findings) == 0 {
		return nil
	}
	out := make([]store.Finding, 0, len(findings))
	for _, f := range findings {
		out = append(out, store.Finding{
			Candidate:   f.Candidate,
			Registrable: f.Registrable,
			Technique:   f.Technique,
			Registered:  f.Registered,
			HasNS:       f.HasNS,
			HasA:        f.HasA,
			HasMX:       f.HasMX,
			Wildcard:    f.Wildcard,
			Addresses:   f.Addresses,
			NS:          f.NS,
			MX:          f.MX,
			Rcode:       f.Rcode,
			Error:       f.Error,
		})
	}
	return out
}
