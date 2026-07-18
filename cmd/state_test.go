package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/andoniaf/yatt/internal/scan"
	"github.com/andoniaf/yatt/internal/store"
)

// decodeFindings parses the JSON output of a scan or diff command.
func decodeFindings(t *testing.T, stdout string) []scan.Finding {
	t.Helper()

	var findings []scan.Finding
	if err := json.Unmarshal([]byte(stdout), &findings); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, stdout)
	}
	return findings
}

func countDiff(findings []scan.Finding, status scan.DiffStatus) int {
	var n int
	for _, f := range findings {
		if f.Diff == status {
			n++
		}
	}
	return n
}

// The headline behaviour of this phase: the first scan of a seed is all new,
// and an identical second scan reports nothing new.
func TestSecondScanReportsNothingNew(t *testing.T) {
	withTempStore(t)
	withFakeResolver(t, scriptedResolver{registered: map[string]bool{"xample.com": true}})

	first := decodeFindings(t, mustRun(t, "scan", "example.com", "--output", "json"))
	if len(first) == 0 {
		t.Fatal("first scan produced no findings")
	}
	if got := countDiff(first, scan.DiffNew); got != len(first) {
		t.Errorf("first scan marked %d of %d findings new, want all of them", got, len(first))
	}

	second := decodeFindings(t, mustRun(t, "scan", "example.com", "--output", "json"))
	if got := countDiff(second, scan.DiffNew); got != 0 {
		t.Errorf("second scan marked %d findings new, want none", got)
	}
	if got := countDiff(second, scan.DiffUnchanged); got != len(second) {
		t.Errorf("second scan marked %d of %d findings unchanged, want all of them", got, len(second))
	}
}

// A candidate that becomes registered between two scans is the signal this tool
// exists to surface, so it must come through as changed rather than as noise.
func TestScanReportsASignalFlipAsChanged(t *testing.T) {
	withTempStore(t)

	registered := map[string]bool{}
	withFakeResolver(t, scriptedResolver{registered: registered})

	mustRun(t, "scan", "example.com", "--output", "json")

	registered["xample.com"] = true
	second := decodeFindings(t, mustRun(t, "scan", "example.com", "--output", "json"))

	var changed []string
	for _, f := range second {
		if f.Diff == scan.DiffChanged {
			changed = append(changed, f.Candidate)
		}
	}
	if len(changed) != 1 || changed[0] != "xample.com" {
		t.Errorf("changed candidates = %v, want just xample.com", changed)
	}
}

func TestHistoryListsEveryScan(t *testing.T) {
	withTempStore(t)
	withFakeResolver(t, scriptedResolver{})

	mustRun(t, "scan", "example.com")
	mustRun(t, "scan", "example.com")

	stdout := mustRun(t, "history", "example.com", "--output", "json")
	var scans []store.Scan
	if err := json.Unmarshal([]byte(stdout), &scans); err != nil {
		t.Fatalf("history output is not valid JSON: %v\n%s", err, stdout)
	}
	if len(scans) != 2 {
		t.Fatalf("history listed %d scans, want 2", len(scans))
	}
	// Most recent first.
	if scans[0].ID <= scans[1].ID {
		t.Errorf("history is not most-recent-first: %d then %d", scans[0].ID, scans[1].ID)
	}
	if scans[0].Candidates == 0 {
		t.Error("history reports no candidates for a scan that produced findings")
	}
}

func TestHistoryOfAnUnscannedSeedIsEmptyNotAnError(t *testing.T) {
	withTempStore(t)
	withFakeResolver(t, scriptedResolver{})

	stdout, stderr, err := run(t, "history", "never-scanned.com", "--output", "json")
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if got := strings.TrimSpace(stdout); got != "[]" {
		t.Errorf("stdout = %q, want %q", got, "[]")
	}
	if !strings.Contains(stderr, "no recorded scans") {
		t.Errorf("stderr = %q, want an explanatory note", stderr)
	}
}

func TestDiffReportsTheDeltaWithoutResolving(t *testing.T) {
	withTempStore(t)

	registered := map[string]bool{}
	withFakeResolver(t, scriptedResolver{registered: registered})

	mustRun(t, "scan", "example.com")
	registered["xample.com"] = true
	mustRun(t, "scan", "example.com")

	// Any attempt to resolve from here on fails the test: diff must answer from
	// the store alone.
	withForbiddenResolver(t)

	changes := decodeFindings(t, mustRun(t, "diff", "example.com", "--output", "json"))
	if len(changes) != 1 {
		t.Fatalf("diff reported %d changes, want 1:\n%+v", len(changes), changes)
	}
	if changes[0].Candidate != "xample.com" || changes[0].Diff != scan.DiffChanged {
		t.Errorf("diff reported %+v, want xample.com as changed", changes[0])
	}
}

func TestDiffOfASingleScanReportsEverythingNew(t *testing.T) {
	withTempStore(t)
	withFakeResolver(t, scriptedResolver{})

	mustRun(t, "scan", "example.com")

	stdout, stderr, err := run(t, "diff", "example.com", "--output", "json")
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	changes := decodeFindings(t, stdout)
	if len(changes) == 0 {
		t.Fatal("diff of a first scan reported nothing, want every candidate as new")
	}
	for _, f := range changes {
		if f.Diff != scan.DiffNew {
			t.Errorf("%s: diff = %q, want %q", f.Candidate, f.Diff, scan.DiffNew)
		}
	}
	if !strings.Contains(stderr, "first recorded scan") {
		t.Errorf("stderr = %q, want a note that there is nothing to compare against", stderr)
	}
}

func TestDiffOfAnUnscannedSeedFails(t *testing.T) {
	withTempStore(t)
	withFakeResolver(t, scriptedResolver{})

	_, _, err := run(t, "diff", "never-scanned.com")
	if err == nil {
		t.Fatal("diff of an unscanned seed succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "no recorded scans") {
		t.Errorf("error = %q, want it to name the missing history", err)
	}
}

// mustRun runs a command that is expected to succeed and returns its stdout.
func mustRun(t *testing.T, args ...string) string {
	t.Helper()

	stdout, stderr, err := run(t, args...)
	if err != nil {
		t.Fatalf("run(%v): %v\n%s", args, err, stderr)
	}
	return stdout
}
