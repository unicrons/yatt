package data

// This file is hand-maintained and deliberately separate from tlds_iana.go:
// that file is regenerated wholesale by `go generate ./pkg/engine/data`, so
// anything living in it is silently discarded on the next run.

// IANATLDSet is IANATLDs as a set, for O(1) membership checks.
var IANATLDSet = buildIANATLDSet()

func buildIANATLDSet() map[string]bool {
	out := make(map[string]bool, len(IANATLDs))
	for _, t := range IANATLDs {
		out[t] = true
	}
	return out
}
