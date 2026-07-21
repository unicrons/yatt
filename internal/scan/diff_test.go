package scan_test

import (
	"testing"

	"github.com/andoniaf/yatt/internal/scan"
)

// finding builds a finding with just the fields the diff looks at.
func finding(candidate string, registered, hasNS, hasA, hasMX bool) scan.Finding {
	return scan.Finding{
		Candidate:   candidate,
		Registrable: candidate,
		Technique:   "omission",
		Registered:  registered,
		HasNS:       hasNS,
		HasA:        hasA,
		HasMX:       hasMX,
	}
}

func statuses(findings []scan.Finding) map[string]scan.DiffStatus {
	out := make(map[string]scan.DiffStatus, len(findings))
	for _, f := range findings {
		out[f.Candidate] = f.Diff
	}
	return out
}

func TestDiff(t *testing.T) {
	tests := []struct {
		name     string
		prior    []scan.Finding
		current  []scan.Finding
		want     map[string]scan.DiffStatus
		wantGone []string
	}{
		{
			name:    "a first scan marks every candidate new",
			prior:   nil,
			current: []scan.Finding{finding("xample.com", false, false, false, false)},
			want:    map[string]scan.DiffStatus{"xample.com": scan.DiffNew},
		},
		{
			name:    "identical signals are unchanged",
			prior:   []scan.Finding{finding("xample.com", true, true, true, false)},
			current: []scan.Finding{finding("xample.com", true, true, true, false)},
			want:    map[string]scan.DiffStatus{"xample.com": scan.DiffUnchanged},
		},
		{
			name:    "a candidate absent from the prior scan is new",
			prior:   []scan.Finding{finding("xample.com", false, false, false, false)},
			current: []scan.Finding{finding("xample.com", false, false, false, false), finding("eample.com", false, false, false, false)},
			want: map[string]scan.DiffStatus{
				"xample.com": scan.DiffUnchanged,
				"eample.com": scan.DiffNew,
			},
		},
		{
			name:     "a candidate absent from the current scan is gone",
			prior:    []scan.Finding{finding("xample.com", true, true, false, false), finding("eample.com", false, false, false, false)},
			current:  []scan.Finding{finding("xample.com", true, true, false, false)},
			want:     map[string]scan.DiffStatus{"xample.com": scan.DiffUnchanged},
			wantGone: []string{"eample.com"},
		},
		{
			name:    "becoming registered is a change",
			prior:   []scan.Finding{finding("xample.com", false, false, false, false)},
			current: []scan.Finding{finding("xample.com", true, true, false, false)},
			want:    map[string]scan.DiffStatus{"xample.com": scan.DiffChanged},
		},
		{
			name:    "gaining MX is a change",
			prior:   []scan.Finding{finding("xample.com", true, true, true, false)},
			current: []scan.Finding{finding("xample.com", true, true, true, true)},
			want:    map[string]scan.DiffStatus{"xample.com": scan.DiffChanged},
		},
		{
			name:    "lapsing is a change, not a disappearance",
			prior:   []scan.Finding{finding("xample.com", true, true, true, true)},
			current: []scan.Finding{finding("xample.com", false, false, false, false)},
			want:    map[string]scan.DiffStatus{"xample.com": scan.DiffChanged},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scan.Diff(tt.prior, tt.current)

			if len(got.Findings) != len(tt.current) {
				t.Fatalf("got %d findings, want %d", len(got.Findings), len(tt.current))
			}
			for candidate, want := range tt.want {
				if status := statuses(got.Findings)[candidate]; status != want {
					t.Errorf("%s: diff = %q, want %q", candidate, status, want)
				}
			}

			if len(got.Gone) != len(tt.wantGone) {
				t.Fatalf("got %d gone findings, want %d", len(got.Gone), len(tt.wantGone))
			}
			for i, candidate := range tt.wantGone {
				if got.Gone[i].Candidate != candidate {
					t.Errorf("gone[%d] = %q, want %q", i, got.Gone[i].Candidate, candidate)
				}
				if got.Gone[i].Diff != scan.DiffGone {
					t.Errorf("gone[%d] diff = %q, want %q", i, got.Gone[i].Diff, scan.DiffGone)
				}
			}
		})
	}
}

// Reordering is the failure mode a positional diff would report as wholesale
// change, which would make every diff useless the moment a technique or a cap
// changes the candidate ordering.
func TestDiffIsKeyedOnCandidateNotPosition(t *testing.T) {
	prior := []scan.Finding{
		finding("a.com", true, true, false, false),
		finding("b.com", false, false, false, false),
	}
	current := []scan.Finding{
		finding("b.com", false, false, false, false),
		finding("a.com", true, true, false, false),
	}

	got := scan.Diff(prior, current)
	for _, f := range got.Findings {
		if f.Diff != scan.DiffUnchanged {
			t.Errorf("%s: diff = %q, want %q", f.Candidate, f.Diff, scan.DiffUnchanged)
		}
	}
	if len(got.Gone) != 0 {
		t.Errorf("got %d gone findings, want none", len(got.Gone))
	}
}

// Address churn on CDN- and cloud-hosted look-alikes is constant; reporting it
// as a change would bury the transitions that matter.
func TestDiffIgnoresRecordChurn(t *testing.T) {
	prior := []scan.Finding{finding("xample.com", true, true, true, false)}
	prior[0].Addresses = []string{"192.0.2.1"}
	current := []scan.Finding{finding("xample.com", true, true, true, false)}
	current[0].Addresses = []string{"198.51.100.9"}

	if got := scan.Diff(prior, current); got.Findings[0].Diff != scan.DiffUnchanged {
		t.Errorf("diff = %q, want %q", got.Findings[0].Diff, scan.DiffUnchanged)
	}
}

// A failed lookup stores zero-valued signals next to its error. Comparing
// those zeroes as answers would report a phantom lapse on the failure and a
// phantom change back on recovery — a failed lookup is not evidence that
// anything moved.
func TestDiffTreatsErroredLookupsAsUnchanged(t *testing.T) {
	registered := finding("xample.com", true, true, true, false)
	errored := finding("xample.com", false, false, false, false)
	errored.Error = "query NS xample.com: i/o timeout"

	if got := scan.Diff([]scan.Finding{registered}, []scan.Finding{errored}); got.Findings[0].Diff != scan.DiffUnchanged {
		t.Errorf("registered -> errored: diff = %q, want %q", got.Findings[0].Diff, scan.DiffUnchanged)
	}
	if got := scan.Diff([]scan.Finding{errored}, []scan.Finding{registered}); got.Findings[0].Diff != scan.DiffUnchanged {
		t.Errorf("errored -> recovered: diff = %q, want %q", got.Findings[0].Diff, scan.DiffUnchanged)
	}
}

func TestDiffDoesNotMutateInput(t *testing.T) {
	current := []scan.Finding{finding("xample.com", false, false, false, false)}

	scan.Diff(nil, current)

	if current[0].Diff != "" {
		t.Errorf("Diff mutated its input: diff = %q, want empty", current[0].Diff)
	}
}

func TestChangesOmitsUnchangedAndOrdersByStatus(t *testing.T) {
	prior := []scan.Finding{
		finding("same.com", false, false, false, false),
		finding("moved.com", false, false, false, false),
		finding("lapsed.com", true, true, false, false),
	}
	current := []scan.Finding{
		finding("same.com", false, false, false, false),
		finding("moved.com", true, true, false, false),
		finding("fresh.com", false, false, false, false),
	}

	changes := scan.Changes(scan.Diff(prior, current))

	var got []string
	for _, f := range changes {
		got = append(got, f.Candidate)
	}
	want := []string{"fresh.com", "moved.com", "lapsed.com"}
	if len(got) != len(want) {
		t.Fatalf("changes = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("changes = %v, want %v", got, want)
			break
		}
	}
}
