package scan_test

import (
	"reflect"
	"testing"

	"github.com/unicrons/yatt/internal/scan"
	"github.com/unicrons/yatt/internal/triage"
	"github.com/unicrons/yatt/pkg/engine"
)

// seedRow builds the seed's own row, the way scan.Run emits it: labelled with
// the original technique and carrying the same signals as any candidate.
func seedRow(candidate string, registered, hasNS, hasA, hasMX bool) scan.Finding {
	f := finding(candidate, registered, hasNS, hasA, hasMX)
	f.Technique = engine.TechniqueOriginal
	return f
}

func TestIsOriginal(t *testing.T) {
	tests := []struct {
		name      string
		technique string
		want      bool
	}{
		{name: "the original technique", technique: engine.TechniqueOriginal, want: true},
		{name: "a generated candidate", technique: "omission", want: false},
		{name: "a stateless run with no technique recorded", technique: "", want: false},
		{name: "a technique whose name merely contains it", technique: "originals", want: false},
		{name: "case matters", technique: "Original", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := scan.Finding{Candidate: "example.com", Technique: tt.technique}
			if got := f.IsOriginal(); got != tt.want {
				t.Errorf("IsOriginal() with technique %q = %v, want %v", tt.technique, got, tt.want)
			}
		})
	}
}

func TestSeedFirst(t *testing.T) {
	tests := []struct {
		name     string
		findings []scan.Finding
		want     []string
	}{
		{
			name:     "empty",
			findings: []scan.Finding{},
			want:     []string{},
		},
		{
			name:     "nil",
			findings: nil,
			want:     []string{},
		},
		{
			name: "the seed already leads",
			findings: []scan.Finding{
				seedRow("example.com", true, true, true, true),
				finding("xample.com", false, false, false, false),
				finding("eample.com", false, false, false, false),
			},
			want: []string{"example.com", "xample.com", "eample.com"},
		},
		{
			name: "the seed alone",
			findings: []scan.Finding{
				seedRow("example.com", true, true, true, true),
			},
			want: []string{"example.com"},
		},
		{
			name: "the seed in the middle",
			findings: []scan.Finding{
				finding("xample.com", false, false, false, false),
				seedRow("example.com", true, true, true, true),
				finding("eample.com", false, false, false, false),
			},
			want: []string{"example.com", "xample.com", "eample.com"},
		},
		{
			name: "the seed last",
			findings: []scan.Finding{
				finding("xample.com", false, false, false, false),
				finding("eample.com", false, false, false, false),
				seedRow("example.com", true, true, true, true),
			},
			want: []string{"example.com", "xample.com", "eample.com"},
		},
		{
			// A stateless or filtered report may carry no seed at all; that is not
			// an error and must not reorder anything.
			name: "the seed is absent",
			findings: []scan.Finding{
				finding("xample.com", false, false, false, false),
				finding("eample.com", false, false, false, false),
			},
			want: []string{"xample.com", "eample.com"},
		},
		{
			// The candidates' own order is a deliberate, deterministic product of
			// the technique list; moving the seed must not disturb it.
			name: "the candidates keep their relative order",
			findings: []scan.Finding{
				finding("a.com", false, false, false, false),
				finding("b.com", false, false, false, false),
				seedRow("example.com", true, true, true, true),
				finding("c.com", false, false, false, false),
				finding("d.com", false, false, false, false),
			},
			want: []string{"example.com", "a.com", "b.com", "c.com", "d.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := candidates(scan.SeedFirst(tt.findings))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("SeedFirst() = %v, want %v", got, tt.want)
			}
		})
	}
}

// The common case allocates nothing: a report whose seed already leads, or which
// has no seed at all, is handed straight back.
func TestSeedFirstReturnsTheInputUntouchedWhenThereIsNothingToMove(t *testing.T) {
	tests := []struct {
		name     string
		findings []scan.Finding
	}{
		{
			name: "the seed already leads",
			findings: []scan.Finding{
				seedRow("example.com", true, true, true, true),
				finding("xample.com", false, false, false, false),
			},
		},
		{
			name: "the seed is absent",
			findings: []scan.Finding{
				finding("xample.com", false, false, false, false),
				finding("eample.com", false, false, false, false),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scan.SeedFirst(tt.findings)
			if len(got) != len(tt.findings) {
				t.Fatalf("SeedFirst() returned %d findings, want %d", len(got), len(tt.findings))
			}
			if &got[0] != &tt.findings[0] {
				t.Error("SeedFirst() copied a slice it did not need to reorder")
			}
		})
	}
}

func TestSeedFirstDoesNotMutateItsInput(t *testing.T) {
	findings := []scan.Finding{
		finding("xample.com", false, false, false, false),
		seedRow("example.com", true, true, true, true),
		finding("eample.com", false, false, false, false),
	}
	before := append([]scan.Finding(nil), findings...)

	scan.SeedFirst(findings)

	if !reflect.DeepEqual(findings, before) {
		t.Errorf("SeedFirst mutated its input: %v, want %v", candidates(findings), candidates(before))
	}
}

// The seed is the baseline every candidate is read against, so a filter that
// would otherwise drop it must not: a report saying "this look-alike has MX" is
// worth nothing without the row saying whether the real domain does.
func TestTriageFilterAlwaysKeepsTheSeed(t *testing.T) {
	seed := scan.Finding{Candidate: "example.com", Technique: engine.TechniqueOriginal}
	owned := scan.Finding{Candidate: "xample.com", Technique: "omission", Triage: triage.StatusOwned}
	fresh := scan.Finding{Candidate: "eample.com", Technique: "omission", Triage: triage.StatusNew}

	tests := []struct {
		name   string
		filter scan.TriageFilter
		want   []string
	}{
		{
			name:   "an include filter the seed's blank verdict does not match",
			filter: scan.TriageFilter{Include: []triage.Status{triage.StatusOwned}},
			want:   []string{"example.com", "xample.com"},
		},
		{
			// The seed's verdict is left blank, which the filter reads as "new",
			// so this is exactly the filter that would drop it.
			name:   "an exclude filter that covers the seed's blank verdict",
			filter: scan.TriageFilter{Exclude: []triage.Status{triage.StatusNew}},
			want:   []string{"example.com", "xample.com"},
		},
		{
			name:   "a filter that matches no candidate at all",
			filter: scan.TriageFilter{Include: []triage.Status{triage.StatusFalsePositive}},
			want:   []string{"example.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := candidates(tt.filter.Apply([]scan.Finding{seed, owned, fresh}))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Apply() = %v, want %v", got, tt.want)
			}
		})
	}
}

// An explicit verdict on the seed is honoured, but it still cannot hide the row.
func TestTriageFilterKeepsTheSeedEvenWithItsOwnVerdict(t *testing.T) {
	seed := scan.Finding{
		Candidate: "example.com",
		Technique: engine.TechniqueOriginal,
		Triage:    triage.StatusOwned,
	}
	filter := scan.TriageFilter{Exclude: []triage.Status{triage.StatusOwned}}

	if got := candidates(filter.Apply([]scan.Finding{seed})); !reflect.DeepEqual(got, []string{"example.com"}) {
		t.Errorf("Apply() = %v, want the seed kept despite the exclusion", got)
	}
}

func TestChangesLeadsWithTheSeed(t *testing.T) {
	tests := []struct {
		name    string
		prior   []scan.Finding
		current []scan.Finding
		want    []string
	}{
		{
			// The headline case: the seed leads even though it did not move,
			// because it is what the changes below it are read against.
			name: "the seed leads an unchanged report",
			prior: []scan.Finding{
				seedRow("example.com", true, true, true, true),
				finding("xample.com", false, false, false, false),
			},
			current: []scan.Finding{
				seedRow("example.com", true, true, true, true),
				finding("xample.com", true, true, false, false),
			},
			want: []string{"example.com", "xample.com"},
		},
		{
			name: "the seed leads when nothing moved at all",
			prior: []scan.Finding{
				seedRow("example.com", true, true, true, true),
				finding("xample.com", false, false, false, false),
			},
			current: []scan.Finding{
				seedRow("example.com", true, true, true, true),
				finding("xample.com", false, false, false, false),
			},
			want: []string{"example.com"},
		},
		{
			name: "the seed leads even when it is the thing that moved",
			prior: []scan.Finding{
				seedRow("example.com", true, true, true, true),
				finding("xample.com", false, false, false, false),
			},
			current: []scan.Finding{
				seedRow("example.com", true, true, true, false),
				finding("xample.com", false, false, false, false),
			},
			want: []string{"example.com"},
		},
		{
			name: "the seed is not listed again among the changes it moved with",
			prior: []scan.Finding{
				seedRow("example.com", true, true, true, true),
				finding("xample.com", false, false, false, false),
			},
			current: []scan.Finding{
				seedRow("example.com", true, true, true, false),
				finding("xample.com", true, true, false, false),
				finding("eample.com", false, false, false, false),
			},
			want: []string{"example.com", "eample.com", "xample.com"},
		},
		{
			// A first scan marks the seed new like everything else; it still gets
			// exactly one row, at the top.
			name:  "the seed is not listed twice on a first scan",
			prior: nil,
			current: []scan.Finding{
				seedRow("example.com", true, true, true, true),
				finding("xample.com", false, false, false, false),
			},
			want: []string{"example.com", "xample.com"},
		},
		{
			name: "a seed that lapsed out of the current scan is not carried over as gone",
			prior: []scan.Finding{
				seedRow("example.com", true, true, true, true),
				finding("xample.com", false, false, false, false),
			},
			current: []scan.Finding{
				finding("xample.com", false, false, false, false),
			},
			want: []string{},
		},
		{
			name: "a report with no seed reads exactly as it did before",
			prior: []scan.Finding{
				finding("xample.com", false, false, false, false),
			},
			current: []scan.Finding{
				finding("xample.com", true, true, false, false),
			},
			want: []string{"xample.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scan.Changes(scan.Diff(tt.prior, tt.current))

			if names := candidates(got); !reflect.DeepEqual(names, tt.want) {
				t.Fatalf("Changes() = %v, want %v", names, tt.want)
			}

			var seeds int
			for _, f := range got {
				if f.IsOriginal() {
					seeds++
				}
			}
			if seeds > 1 {
				t.Errorf("Changes() listed the seed %d times, want at most once", seeds)
			}
		})
	}
}

// The reason HasChanges exists: Changes always emits the seed's row, so a report
// of exactly one row means "nothing changed", not "one thing changed".
func TestHasChanges(t *testing.T) {
	tests := []struct {
		name    string
		prior   []scan.Finding
		current []scan.Finding
		want    bool
	}{
		{
			name:    "a first scan is all news",
			prior:   nil,
			current: []scan.Finding{seedRow("example.com", true, true, true, true)},
			want:    true,
		},
		{
			name:    "only the seed, and it did not move",
			prior:   []scan.Finding{seedRow("example.com", true, true, true, true)},
			current: []scan.Finding{seedRow("example.com", true, true, true, true)},
			want:    false,
		},
		{
			name: "the seed and unchanged candidates",
			prior: []scan.Finding{
				seedRow("example.com", true, true, true, true),
				finding("xample.com", false, false, false, false),
			},
			current: []scan.Finding{
				seedRow("example.com", true, true, true, true),
				finding("xample.com", false, false, false, false),
			},
			want: false,
		},
		{
			name: "a new candidate",
			prior: []scan.Finding{
				seedRow("example.com", true, true, true, true),
			},
			current: []scan.Finding{
				seedRow("example.com", true, true, true, true),
				finding("xample.com", false, false, false, false),
			},
			want: true,
		},
		{
			name: "a changed candidate",
			prior: []scan.Finding{
				seedRow("example.com", true, true, true, true),
				finding("xample.com", false, false, false, false),
			},
			current: []scan.Finding{
				seedRow("example.com", true, true, true, true),
				finding("xample.com", true, true, false, false),
			},
			want: true,
		},
		{
			name: "a gone candidate",
			prior: []scan.Finding{
				seedRow("example.com", true, true, true, true),
				finding("xample.com", true, true, false, false),
			},
			current: []scan.Finding{
				seedRow("example.com", true, true, true, true),
			},
			want: true,
		},
		{
			// A change to the domain being protected — losing its MX, say — is
			// news in its own right.
			name: "a movement in the seed's own signals",
			prior: []scan.Finding{
				seedRow("example.com", true, true, true, true),
				finding("xample.com", false, false, false, false),
			},
			current: []scan.Finding{
				seedRow("example.com", true, true, true, false),
				finding("xample.com", false, false, false, false),
			},
			want: true,
		},
		{
			name: "address churn on an otherwise unchanged report",
			prior: []scan.Finding{
				seedRow("example.com", true, true, true, true),
				finding("xample.com", true, true, true, false),
			},
			current: []scan.Finding{
				seedRow("example.com", true, true, true, true),
				finding("xample.com", true, true, true, false),
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := scan.Diff(tt.prior, tt.current)
			if got := scan.HasChanges(result); got != tt.want {
				t.Errorf("HasChanges() = %v, want %v (Changes() = %v)",
					got, tt.want, candidates(scan.Changes(result)))
			}
		})
	}
}

// The guard the length test can no longer be: a report with nothing to say is
// one row long, and only HasChanges can tell that apart from one real change.
func TestHasChangesDisagreesWithTheLengthOfChanges(t *testing.T) {
	prior := []scan.Finding{
		seedRow("example.com", true, true, true, true),
		finding("xample.com", false, false, false, false),
	}
	current := []scan.Finding{
		seedRow("example.com", true, true, true, true),
		finding("xample.com", false, false, false, false),
	}

	result := scan.Diff(prior, current)
	if got := len(scan.Changes(result)); got != 1 {
		t.Fatalf("Changes() returned %d rows, want just the seed's", got)
	}
	if scan.HasChanges(result) {
		t.Error("HasChanges() = true, want false — the only row is the seed's, and it did not move")
	}
}
