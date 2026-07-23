package scan_test

import (
	"testing"

	"github.com/unicrons/yatt/internal/scan"
	"github.com/unicrons/yatt/internal/triage"
)

func filterFindings() []scan.Finding {
	return []scan.Finding{
		{Candidate: "a.com", Triage: triage.StatusNew},
		{Candidate: "b.com", Triage: triage.StatusOwned},
		{Candidate: "c.com", Triage: triage.StatusMalicious},
		{Candidate: "d.com", Triage: triage.StatusOwned},
		{Candidate: "e.com"}, // a stateless run: no verdict read at all
	}
}

func candidates(findings []scan.Finding) []string {
	out := make([]string, 0, len(findings))
	for _, f := range findings {
		out = append(out, f.Candidate)
	}
	return out
}

func TestTriageFilterApply(t *testing.T) {
	tests := []struct {
		name   string
		filter scan.TriageFilter
		want   []string
	}{
		{
			name:   "an empty filter keeps everything",
			filter: scan.TriageFilter{},
			want:   []string{"a.com", "b.com", "c.com", "d.com", "e.com"},
		},
		{
			name:   "include narrows to one status",
			filter: scan.TriageFilter{Include: []triage.Status{triage.StatusOwned}},
			want:   []string{"b.com", "d.com"},
		},
		{
			name: "include accepts several statuses",
			filter: scan.TriageFilter{
				Include: []triage.Status{triage.StatusOwned, triage.StatusMalicious},
			},
			want: []string{"b.com", "c.com", "d.com"},
		},
		{
			// The headline case: hide the domains we registered ourselves.
			name:   "exclude drops one status",
			filter: scan.TriageFilter{Exclude: []triage.Status{triage.StatusOwned}},
			want:   []string{"a.com", "c.com", "e.com"},
		},
		{
			name:   "include new selects exactly the untriaged backlog",
			filter: scan.TriageFilter{Include: []triage.Status{triage.StatusNew}},
			want:   []string{"a.com", "e.com"},
		},
		{
			name: "exclude wins over include",
			filter: scan.TriageFilter{
				Include: []triage.Status{triage.StatusOwned, triage.StatusMalicious},
				Exclude: []triage.Status{triage.StatusOwned},
			},
			want: []string{"c.com"},
		},
		{
			name:   "a filter matching nothing yields nothing",
			filter: scan.TriageFilter{Include: []triage.Status{triage.StatusFalsePositive}},
			want:   []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := candidates(tt.filter.Apply(filterFindings()))
			if len(got) != len(tt.want) {
				t.Fatalf("Apply() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("Apply()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// The report promises that a candidate whose resolution failed is always
// shown. An errored row carries the implicit "new" status, so without an
// exemption `--status malicious` would silently eat it and a partially-failed
// scan would render as confidently complete.
func TestTriageFilterKeepsErroredRows(t *testing.T) {
	findings := []scan.Finding{
		{Candidate: "a.com", Triage: triage.StatusMalicious},
		{Candidate: "b.com", Triage: triage.StatusNew, Error: "query NS b.com: i/o timeout"},
		{Candidate: "c.com", Triage: triage.StatusNew},
	}
	filter := scan.TriageFilter{Include: []triage.Status{triage.StatusMalicious}}

	got := candidates(filter.Apply(findings))
	want := []string{"a.com", "b.com"}
	if len(got) != len(want) {
		t.Fatalf("Apply() = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("Apply()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// An unset verdict is what a stateless run produces. Treating it as unjudged
// rather than as unmatchable keeps `--status new` honest and stops a filtered
// stateless run from silently reporting nothing.
func TestTriageFilterTreatsAnUnsetStatusAsNew(t *testing.T) {
	filter := scan.TriageFilter{Include: []triage.Status{triage.StatusNew}}
	if !filter.Match("") {
		t.Error("Match(\"\") = false, want an unset verdict to count as new")
	}
}

func TestTriageFilterEmpty(t *testing.T) {
	if !(scan.TriageFilter{}).Empty() {
		t.Error("a zero TriageFilter is not reported as empty")
	}
	if (scan.TriageFilter{Exclude: []triage.Status{triage.StatusOwned}}).Empty() {
		t.Error("a filter with an exclusion is reported as empty")
	}
}
