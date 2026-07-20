package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/andoniaf/yatt/internal/scan"
	"github.com/andoniaf/yatt/internal/store"
	"github.com/andoniaf/yatt/pkg/engine"
)

// seedCandidateCount is the number of look-alikes omission produces for
// "example": one per position, all distinct.
const seedCandidateCount = 7

// countSeedRows reports how many of the findings are the seed's own row.
func countSeedRows(findings []scan.Finding) int {
	var n int
	for _, f := range findings {
		if f.IsOriginal() {
			n++
		}
	}
	return n
}

// The guarantee the whole feature rests on, asserted at the boundary a user
// actually reads: whatever the format, the seed is the first thing on stdout.
func TestScanReportsTheSeedFirstInEveryFormat(t *testing.T) {
	tests := []struct {
		name   string
		format string
		// first extracts the first reported candidate name from stdout.
		first func(t *testing.T, stdout string) (candidate, technique string)
	}{
		{
			name:   "table",
			format: "table",
			first: func(t *testing.T, stdout string) (string, string) {
				t.Helper()

				lines := strings.Split(strings.TrimSpace(stdout), "\n")
				if len(lines) < 2 {
					t.Fatalf("table has no rows below the header:\n%s", stdout)
				}
				fields := strings.Fields(lines[1])
				if len(fields) < 2 {
					t.Fatalf("first row %q has too few columns", lines[1])
				}
				return fields[0], fields[1]
			},
		},
		{
			name:   "json",
			format: "json",
			first: func(t *testing.T, stdout string) (string, string) {
				t.Helper()

				findings := decodeFindings(t, stdout)
				if len(findings) == 0 {
					t.Fatal("no findings")
				}
				return findings[0].Candidate, findings[0].Technique
			},
		},
		{
			name:   "ndjson",
			format: "ndjson",
			first: func(t *testing.T, stdout string) (string, string) {
				t.Helper()

				lines := strings.Split(strings.TrimSpace(stdout), "\n")
				if len(lines) == 0 || lines[0] == "" {
					t.Fatalf("no lines on stdout:\n%s", stdout)
				}
				var f scan.Finding
				if err := json.Unmarshal([]byte(lines[0]), &f); err != nil {
					t.Fatalf("first line is not valid JSON: %v\n%s", err, lines[0])
				}
				return f.Candidate, f.Technique
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withTempStore(t)
			withFakeResolver(t, scriptedResolver{registered: map[string]bool{
				"example.com": true,
				"xample.com":  true,
			}})

			candidate, technique := tt.first(t, mustRun(t, "scan", "example.com", "--output", tt.format, "--technique", "omission"))
			if candidate != "example.com" {
				t.Errorf("first row = %q, want the seed example.com", candidate)
			}
			if technique != engine.TechniqueOriginal {
				t.Errorf("first row technique = %q, want %q", technique, engine.TechniqueOriginal)
			}
		})
	}
}

// The seed is resolved and reported like a candidate, but it is still one row,
// not two: a technique landing back on the seed's own name must not double it.
func TestScanDoesNotDuplicateTheSeed(t *testing.T) {
	withTempStore(t)
	withFakeResolver(t, scriptedResolver{registered: map[string]bool{"example.com": true}})

	findings := decodeFindings(t, mustRun(t,
		"scan", "example.com", "--output", "json", "--technique", "omission", "--show-unregistered"))

	if len(findings) != seedCandidateCount+1 {
		t.Fatalf("scan reported %d rows, want %d candidates plus the seed: %v",
			len(findings), seedCandidateCount, candidateNames(findings))
	}
	if got := countSeedRows(findings); got != 1 {
		t.Errorf("scan reported %d seed rows, want exactly 1: %v", got, candidateNames(findings))
	}

	var seen int
	for _, f := range findings {
		if f.Candidate == "example.com" {
			seen++
		}
	}
	if seen != 1 {
		t.Errorf("example.com appears %d times, want once", seen)
	}
}

// The seed's row carries real signals rather than a placeholder: without them
// there is nothing to read the candidates against.
func TestScanResolvesTheSeedLikeACandidate(t *testing.T) {
	withTempStore(t)
	withFakeResolver(t, scriptedResolver{registered: map[string]bool{"example.com": true}})

	findings := decodeFindings(t, mustRun(t, "scan", "example.com", "--output", "json", "--technique", "omission"))
	seed := findingFor(t, findings, "example.com")

	if !seed.IsOriginal() {
		t.Errorf("seed row technique = %q, want %q", seed.Technique, engine.TechniqueOriginal)
	}
	if seed.Registrable != "example.com" {
		t.Errorf("seed registrable = %q, want example.com", seed.Registrable)
	}
	if !seed.Registered || !seed.HasNS {
		t.Errorf("seed row = %+v, want the scripted registered signals", seed)
	}
}

// A filtered report is still read against the baseline, so the seed rides along
// with every filter — including the ones that would otherwise select it away.
func TestScanKeepsTheSeedThroughTriageFiltering(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "include a status only one candidate has",
			args: []string{"--status", "owned"},
			want: []string{"example.com", "xample.com"},
		},
		{
			name: "include a status nothing has",
			args: []string{"--status", "false_positive"},
			want: []string{"example.com"},
		},
		{
			// The seed's own verdict is blank, which the filter reads as "new",
			// so this is exactly the filter that would drop it.
			name: "exclude the untriaged backlog",
			args: []string{"--exclude-status", "new"},
			want: []string{"example.com", "xample.com"},
		},
		{
			name: "exclude the only judged candidate",
			args: []string{"--exclude-status", "owned"},
			want: []string{
				"example.com",
				"eample.com", "exmple.com", "exaple.com",
				"examle.com", "exampe.com", "exampl.com",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withTempStore(t)
			withFakeResolver(t, scriptedResolver{registered: map[string]bool{"xample.com": true}})

			// Only the candidate is judged: the seed's own verdict stays blank,
			// which is what makes these filters ones that would otherwise drop it.
			mustRun(t, "scan", "example.com", "--technique", "omission")
			mustRun(t, "triage", "example.com", "xample.com", "--status", "owned")

			args := append([]string{
				"scan", "example.com", "--output", "json", "--technique", "omission", "--show-unregistered",
			}, tt.args...)
			findings := decodeFindings(t, mustRun(t, args...))

			if len(findings) == 0 || !findings[0].IsOriginal() {
				t.Fatalf("filtered report = %v, want it to lead with the seed", candidateNames(findings))
			}
			if got := countSeedRows(findings); got != 1 {
				t.Errorf("filtered report has %d seed rows, want 1: %v", got, candidateNames(findings))
			}
			if got := candidateNames(findings); !equalStrings(got, tt.want) {
				t.Errorf("filtered report = %v, want %v", got, tt.want)
			}
		})
	}
}

// A verdict deliberately recorded against the seed is honoured in the column but
// still cannot hide the row.
func TestScanKeepsTheSeedWhenItsOwnVerdictIsExcluded(t *testing.T) {
	withTempStore(t)
	withFakeResolver(t, scriptedResolver{registered: map[string]bool{"example.com": true}})

	mustRun(t, "scan", "example.com", "--technique", "omission")
	mustRun(t, "triage", "example.com", "example.com", "--status", "owned", "--note", "the real one")

	findings := decodeFindings(t, mustRun(t,
		"scan", "example.com", "--output", "json", "--exclude-status", "owned", "--technique", "omission"))

	if len(findings) == 0 || !findings[0].IsOriginal() {
		t.Fatalf("report = %v, want the seed kept despite the exclusion", candidateNames(findings))
	}
}

// The seed is stored like a candidate so it takes part in the diff, but it is
// not one, and a history listing that counted it would be off by one forever.
func TestHistoryDoesNotCountTheSeedAsACandidate(t *testing.T) {
	withTempStore(t)
	withFakeResolver(t, scriptedResolver{registered: map[string]bool{
		"example.com": true,
		"xample.com":  true,
	}})

	mustRun(t, "scan", "example.com", "--technique", "omission")

	stdout := mustRun(t, "history", "example.com", "--output", "json")
	var scans []store.Scan
	if err := json.Unmarshal([]byte(stdout), &scans); err != nil {
		t.Fatalf("history output is not valid JSON: %v\n%s", err, stdout)
	}
	if len(scans) != 1 {
		t.Fatalf("history listed %d scans, want 1", len(scans))
	}
	if scans[0].Candidates != seedCandidateCount {
		t.Errorf("history reports %d candidates, want %d — the seed was counted as one",
			scans[0].Candidates, seedCandidateCount)
	}
	// Only xample.com is a registered candidate; the seed's own registration is
	// not a look-alike finding.
	if scans[0].Registered != 1 {
		t.Errorf("history reports %d registered candidates, want 1 — the seed was counted",
			scans[0].Registered)
	}
}

// `yatt diff` leads with the seed too, and still does so when the seed is the
// only row it has to show.
func TestDiffLeadsWithTheSeedWhenNothingChanged(t *testing.T) {
	withTempStore(t)
	withFakeResolver(t, scriptedResolver{registered: map[string]bool{"xample.com": true}})

	mustRun(t, "scan", "example.com", "--technique", "omission")
	mustRun(t, "scan", "example.com", "--technique", "omission")

	withForbiddenResolver(t)

	changes := decodeFindings(t, mustRun(t, "diff", "example.com", "--output", "json"))
	if len(changes) != 1 {
		t.Fatalf("diff reported %d rows, want just the seed's: %v", len(changes), candidateNames(changes))
	}
	if !changes[0].IsOriginal() || changes[0].Candidate != "example.com" {
		t.Errorf("diff row = %+v, want the seed's own row", changes[0])
	}
	if changes[0].Diff != scan.DiffUnchanged {
		t.Errorf("seed diff = %q, want %q", changes[0].Diff, scan.DiffUnchanged)
	}
}

// TestDiffReportsNoChangesDespiteTheSeedRow guards the interaction between the
// seed row and the "nothing moved" note.
//
// Because Changes always emits the seed, the note cannot be driven off the row
// count: a one-row report means nothing changed, not that one thing did. Testing
// it through stderr rather than through HasChanges directly is deliberate — the
// bug this replaces was a live function nobody called.
func TestDiffReportsNoChangesDespiteTheSeedRow(t *testing.T) {
	withTempStore(t)
	withFakeResolver(t, scriptedResolver{registered: map[string]bool{"xample.com": true}})

	mustRun(t, "scan", "example.com", "--technique", "omission")
	mustRun(t, "scan", "example.com", "--technique", "omission")

	withForbiddenResolver(t)

	_, stderr, err := run(t, "diff", "example.com")
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if !strings.Contains(stderr, "no changes") {
		t.Errorf("stderr does not report that nothing changed:\n%s", stderr)
	}
}

// equalStrings compares two candidate name lists.
func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
