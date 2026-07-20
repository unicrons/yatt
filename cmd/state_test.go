package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/andoniaf/yatt/internal/scan"
	"github.com/andoniaf/yatt/internal/store"
	"github.com/andoniaf/yatt/internal/triage"
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

	first := decodeFindings(t, mustRun(t, "scan", "example.com", "--output", "json", "--technique", "omission"))
	if len(first) == 0 {
		t.Fatal("first scan produced no findings")
	}
	if got := countDiff(first, scan.DiffNew); got != len(first) {
		t.Errorf("first scan marked %d of %d findings new, want all of them", got, len(first))
	}

	second := decodeFindings(t, mustRun(t, "scan", "example.com", "--output", "json", "--technique", "omission"))
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

	mustRun(t, "scan", "example.com", "--output", "json", "--technique", "omission")

	registered["xample.com"] = true
	second := decodeFindings(t, mustRun(t, "scan", "example.com", "--output", "json", "--technique", "omission"))

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

	mustRun(t, "scan", "example.com", "--technique", "omission")
	mustRun(t, "scan", "example.com", "--technique", "omission")

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

	mustRun(t, "scan", "example.com", "--technique", "omission")
	registered["xample.com"] = true
	mustRun(t, "scan", "example.com", "--technique", "omission")

	// Any attempt to resolve from here on fails the test: diff must answer from
	// the store alone.
	withForbiddenResolver(t)

	// The seed's own row leads the diff and the one changed candidate follows it.
	changes := decodeFindings(t, mustRun(t, "diff", "example.com", "--output", "json"))
	if len(changes) != 2 {
		t.Fatalf("diff reported %d rows, want the seed plus 1 change:\n%+v", len(changes), changes)
	}
	if !changes[0].IsOriginal() {
		t.Errorf("first diff row = %+v, want the seed's own row", changes[0])
	}
	if changes[1].Candidate != "xample.com" || changes[1].Diff != scan.DiffChanged {
		t.Errorf("diff reported %+v, want xample.com as changed", changes[1])
	}
}

func TestDiffOfASingleScanReportsEverythingNew(t *testing.T) {
	withTempStore(t)
	withFakeResolver(t, scriptedResolver{})

	mustRun(t, "scan", "example.com", "--technique", "omission")

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

// findingFor returns the finding for one candidate.
func findingFor(t *testing.T, findings []scan.Finding, candidate string) scan.Finding {
	t.Helper()

	for _, f := range findings {
		if f.Candidate == candidate {
			return f
		}
	}
	t.Fatalf("no finding for %s in %+v", candidate, findings)
	return scan.Finding{}
}

// The headline behaviour of this phase: a verdict recorded once is still
// attached to the candidate on every later scan.
func TestTriageSurvivesRescanning(t *testing.T) {
	withTempStore(t)
	withFakeResolver(t, scriptedResolver{registered: map[string]bool{"xample.com": true}})

	mustRun(t, "scan", "example.com", "--technique", "omission")
	mustRun(t, "triage", "example.com", "xample.com", "--status", "owned", "--note", "defensive registration")

	// --show-unregistered because this also asserts on an untriaged, and
	// therefore unregistered, candidate below.
	findings := decodeFindings(t, mustRun(t,
		"scan", "example.com", "--output", "json", "--technique", "omission", "--show-unregistered"))

	owned := findingFor(t, findings, "xample.com")
	if owned.Triage != triage.StatusOwned {
		t.Errorf("triage = %q, want %q after re-scanning", owned.Triage, triage.StatusOwned)
	}
	if owned.TriageNote != "defensive registration" {
		t.Errorf("triage note = %q, want the note recorded earlier", owned.TriageNote)
	}

	// Everything nobody judged reports as new, so the untriaged backlog is
	// visible rather than blank.
	other := findingFor(t, findings, "eample.com")
	if other.Triage != triage.StatusNew {
		t.Errorf("untriaged candidate triage = %q, want %q", other.Triage, triage.StatusNew)
	}
}

// A verdict is not a DNS signal, so recording one must not make the candidate
// look like it changed on the next scan.
func TestTriagingDoesNotCountAsAChange(t *testing.T) {
	withTempStore(t)
	withFakeResolver(t, scriptedResolver{registered: map[string]bool{"xample.com": true}})

	mustRun(t, "scan", "example.com", "--technique", "omission")
	mustRun(t, "triage", "example.com", "xample.com", "--status", "malicious")

	findings := decodeFindings(t, mustRun(t, "scan", "example.com", "--output", "json", "--technique", "omission"))
	if got := countDiff(findings, scan.DiffChanged); got != 0 {
		t.Errorf("%d findings reported as changed after only a triage verdict was recorded, want 0", got)
	}
}

func TestScanFiltersByTriageStatus(t *testing.T) {
	withTempStore(t)
	withFakeResolver(t, scriptedResolver{registered: map[string]bool{"xample.com": true}})

	mustRun(t, "scan", "example.com", "--technique", "omission")
	mustRun(t, "triage", "example.com", "xample.com", "--status", "owned")

	all := decodeFindings(t, mustRun(t, "scan", "example.com", "--output", "json", "--technique", "omission"))

	// The seed rides along with every filtered report, so the candidate it
	// selected is read against the real domain rather than in isolation.
	only := decodeFindings(t, mustRun(t, "scan", "example.com", "--output", "json", "--status", "owned", "--technique", "omission"))
	if len(only) != 2 || !only[0].IsOriginal() || only[1].Candidate != "xample.com" {
		t.Errorf("--status owned reported %+v, want the seed then xample.com", candidateNames(only))
	}

	// The direction the manual workflow actually needs: everything except the
	// domains we registered ourselves.
	rest := decodeFindings(t, mustRun(t, "scan", "example.com", "--output", "json", "--exclude-status", "owned", "--technique", "omission"))
	if len(rest) != len(all)-1 {
		t.Errorf("--exclude-status owned reported %d findings, want %d", len(rest), len(all)-1)
	}
	for _, f := range rest {
		if f.Candidate == "xample.com" {
			t.Error("--exclude-status owned still reported the owned candidate")
		}
	}
}

// Filtering is a reporting concern. If it reached the store, the next scan
// would report every hidden candidate as gone and then as new again.
func TestScanFilteringDoesNotShrinkTheStoredScan(t *testing.T) {
	withTempStore(t)
	withFakeResolver(t, scriptedResolver{registered: map[string]bool{"xample.com": true}})

	mustRun(t, "scan", "example.com", "--technique", "omission")
	mustRun(t, "triage", "example.com", "xample.com", "--status", "owned")
	mustRun(t, "scan", "example.com", "--status", "owned", "--technique", "omission")

	// The filtered run recorded everything, so the following unfiltered run has
	// nothing new to report.
	findings := decodeFindings(t, mustRun(t, "scan", "example.com", "--output", "json", "--technique", "omission"))
	if got := countDiff(findings, scan.DiffNew); got != 0 {
		t.Errorf("%d findings reported as new after a filtered scan, want 0 — filtering reached the store", got)
	}
	if got := countDiff(findings, scan.DiffGone); got != 0 {
		t.Errorf("%d findings reported as gone after a filtered scan, want 0", got)
	}
}

func TestScanRejectsAnUnknownTriageStatus(t *testing.T) {
	for _, flag := range []string{"--status", "--exclude-status"} {
		t.Run(flag, func(t *testing.T) {
			withTempStore(t)
			withFakeResolver(t, scriptedResolver{})

			_, _, err := run(t, "scan", "example.com", flag, "bogus")
			if err == nil {
				t.Fatalf("scan %s bogus succeeded, want an error", flag)
			}
			if !strings.Contains(err.Error(), "unknown triage status") {
				t.Errorf("error = %q, want it to name the problem", err)
			}
			// The list of valid statuses is the whole point of the error.
			for _, want := range []string{"benign", "malicious", "owned"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want it to list %q", err, want)
				}
			}
		})
	}
}

func TestTriageRejectsAnUnknownStatus(t *testing.T) {
	withTempStore(t)
	withFakeResolver(t, scriptedResolver{})

	mustRun(t, "scan", "example.com", "--technique", "omission")

	_, _, err := run(t, "triage", "example.com", "xample.com", "--status", "spooky")
	if err == nil {
		t.Fatal("triage --status spooky succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "unknown triage status") {
		t.Errorf("error = %q, want it to name the problem", err)
	}
	for _, want := range []string{"benign", "suspicious", "malicious", "watchlist", "ignored", "false_positive", "owned"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to list %q", err, want)
		}
	}
}

// triage must not resolve anything: it records a judgement about a name, and
// the name does not need to exist for the judgement to be worth keeping.
//
// --force is what makes that testable without a scan first, and is also the
// situation it exists for: recording a verdict before the seed has ever been
// scanned must not require the network either.
func TestTriageDoesNotResolve(t *testing.T) {
	withTempStore(t)
	withForbiddenResolver(t)

	mustRun(t, "triage", "example.com", "xample.com", "--status", "benign", "--force")
	mustRun(t, "triage", "list", "example.com")
}

func TestTriageListsRecordedVerdicts(t *testing.T) {
	withTempStore(t)
	withFakeResolver(t, scriptedResolver{})

	mustRun(t, "scan", "example.com", "--technique", "omission")
	mustRun(t, "triage", "example.com", "xample.com", "--status", "owned", "--note", "ours")
	mustRun(t, "triage", "example.com", "eample.com", "--status", "suspicious")

	stdout := mustRun(t, "triage", "list", "example.com", "--output", "json")
	var entries []store.Triage
	if err := json.Unmarshal([]byte(stdout), &entries); err != nil {
		t.Fatalf("triage list output is not valid JSON: %v\n%s", err, stdout)
	}
	if len(entries) != 2 {
		t.Fatalf("listed %d verdicts, want 2", len(entries))
	}
	// Most recently updated first.
	if entries[0].Candidate != "eample.com" {
		t.Errorf("first entry = %q, want the most recently updated (eample.com)", entries[0].Candidate)
	}
}

func TestTriageListOfAnUntriagedSeedIsEmptyNotAnError(t *testing.T) {
	withTempStore(t)
	withFakeResolver(t, scriptedResolver{})

	stdout, stderr, err := run(t, "triage", "list", "never-triaged.com", "--output", "json")
	if err != nil {
		t.Fatalf("triage list: %v", err)
	}
	if got := strings.TrimSpace(stdout); got != "[]" {
		t.Errorf("stdout = %q, want %q", got, "[]")
	}
	if !strings.Contains(stderr, "no verdicts recorded") {
		t.Errorf("stderr = %q, want an explanatory note", stderr)
	}
}

// Re-triaging replaces the verdict rather than adding a second one.
func TestTriageOverwritesAPreviousVerdict(t *testing.T) {
	withTempStore(t)
	withFakeResolver(t, scriptedResolver{})

	mustRun(t, "scan", "example.com", "--technique", "omission")
	mustRun(t, "triage", "example.com", "xample.com", "--status", "suspicious", "--note", "looks parked")
	mustRun(t, "triage", "example.com", "xample.com", "--status", "malicious", "--note", "phishing")

	entries := decodeTriage(t, mustRun(t, "triage", "list", "example.com", "--output", "json"))
	if len(entries) != 1 {
		t.Fatalf("listed %d verdicts, want 1 — re-triaging duplicated the row", len(entries))
	}
	if entries[0].Status != triage.StatusMalicious || entries[0].Note != "phishing" {
		t.Errorf("verdict = %+v, want the second one to have won", entries[0])
	}
}

// An unset flag must never blank a value the user set earlier.
func TestTriageKeepsTheNoteWhenOnlyTheStatusChanges(t *testing.T) {
	withTempStore(t)
	withFakeResolver(t, scriptedResolver{})

	mustRun(t, "scan", "example.com", "--technique", "omission")
	mustRun(t, "triage", "example.com", "xample.com", "--status", "suspicious", "--note", "registered last week")
	mustRun(t, "triage", "example.com", "xample.com", "--status", "malicious")

	entries := decodeTriage(t, mustRun(t, "triage", "list", "example.com", "--output", "json"))
	if entries[0].Note != "registered last week" {
		t.Errorf("note = %q, want the earlier note carried forward", entries[0].Note)
	}

	// An explicit empty note is a deliberate clear, not an omission.
	mustRun(t, "triage", "example.com", "xample.com", "--note", "")
	entries = decodeTriage(t, mustRun(t, "triage", "list", "example.com", "--output", "json"))
	if entries[0].Note != "" {
		t.Errorf("note = %q, want an explicit --note \"\" to clear it", entries[0].Note)
	}
}

// --note alone amends the note and leaves the verdict alone.
func TestTriageNoteAloneKeepsTheStatus(t *testing.T) {
	withTempStore(t)
	withFakeResolver(t, scriptedResolver{})

	mustRun(t, "scan", "example.com", "--technique", "omission")
	mustRun(t, "triage", "example.com", "xample.com", "--status", "watchlist")
	mustRun(t, "triage", "example.com", "xample.com", "--note", "still watching")

	entries := decodeTriage(t, mustRun(t, "triage", "list", "example.com", "--output", "json"))
	if entries[0].Status != triage.StatusWatchlist || entries[0].Note != "still watching" {
		t.Errorf("verdict = %+v, want the watchlist status with an amended note", entries[0])
	}
}

func TestTriageWithoutAStatusOnAnUnjudgedCandidateFails(t *testing.T) {
	withTempStore(t)
	withFakeResolver(t, scriptedResolver{})

	mustRun(t, "scan", "example.com", "--technique", "omission")

	_, _, err := run(t, "triage", "example.com", "xample.com", "--note", "no verdict yet")
	if err == nil {
		t.Fatal("triage with no --status on an unjudged candidate succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "--status") {
		t.Errorf("error = %q, want it to point at the missing flag", err)
	}
}

// "new" records that nobody has judged the candidate, which stops being true
// the moment somebody does.
func TestTriageRefusesToResetACandidateToNew(t *testing.T) {
	withTempStore(t)
	withFakeResolver(t, scriptedResolver{})

	mustRun(t, "scan", "example.com", "--technique", "omission")
	mustRun(t, "triage", "example.com", "xample.com", "--status", "benign")

	_, _, err := run(t, "triage", "example.com", "xample.com", "--status", "new")
	if err == nil {
		t.Fatal("triage --status new succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "new") {
		t.Errorf("error = %q, want it to explain the rejected status", err)
	}
}

func TestTriageNormalizesTheCandidate(t *testing.T) {
	withTempStore(t)
	withFakeResolver(t, scriptedResolver{registered: map[string]bool{"xample.com": true}})

	mustRun(t, "scan", "example.com", "--technique", "omission")
	// A candidate copied out of a report with a trailing dot or in upper case
	// must land on the row the scanner wrote.
	mustRun(t, "triage", "Example.COM.", "XAMPLE.com.", "--status", "owned")

	findings := decodeFindings(t, mustRun(t, "scan", "example.com", "--output", "json", "--technique", "omission"))
	if got := findingFor(t, findings, "xample.com").Triage; got != triage.StatusOwned {
		t.Errorf("triage = %q, want %q — the candidate was not normalized", got, triage.StatusOwned)
	}
}

// The regression this guard exists for: a verdict filed against a seed that can
// never surface the candidate reported success and then vanished, so the analyst
// concluded triage was broken when the pair was simply wrong.
func TestTriageRejectsACandidateThatIsNotTheSeedsAndNamesTheRightSeed(t *testing.T) {
	withTempStore(t)
	withFakeResolver(t, scriptedResolver{})

	// Two seeds, each with its own candidates. "nicrons.cloud" is the omission
	// candidate of unicrons.cloud, and can never appear under example.com.
	mustRun(t, "scan", "example.com", "--technique", "omission")
	mustRun(t, "scan", "unicrons.cloud", "--technique", "omission")

	_, _, err := run(t, "triage", "example.com", "nicrons.cloud", "--status", "owned", "--note", "ours")
	if err == nil {
		t.Fatal("triage of another seed's candidate succeeded, want an error")
	}
	// The error is only worth anything if it turns "this failed" into the
	// command the user meant to type.
	for _, want := range []string{"nicrons.cloud", "example.com", "unicrons.cloud", "--force"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to mention %q", err, want)
		}
	}

	// Nothing may have been written under the wrong seed.
	entries := decodeTriage(t, mustRun(t, "triage", "list", "example.com", "--output", "json"))
	if len(entries) != 0 {
		t.Errorf("rejected triage still stored %+v", entries)
	}
}

// A candidate nobody has ever recorded gets no suggestion, because there is
// none to make — but it must still not be filed silently.
func TestTriageRejectsACandidateNoScanHasEverProduced(t *testing.T) {
	withTempStore(t)
	withFakeResolver(t, scriptedResolver{})

	mustRun(t, "scan", "example.com", "--technique", "omission")

	_, _, err := run(t, "triage", "example.com", "totally-unrelated.test", "--status", "malicious")
	if err == nil {
		t.Fatal("triage of an unknown candidate succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "totally-unrelated.test") || !strings.Contains(err.Error(), "--force") {
		t.Errorf("error = %q, want it to name the candidate and offer the escape hatch", err)
	}
	// With no other seed recording it, the error must not invent one.
	if strings.Contains(err.Error(), "did you mean") {
		t.Errorf("error = %q, want no suggestion when there is no alternate seed", err)
	}
}

// A seed with no history at all is not the candidate's fault, and saying so
// sends the user to `yatt scan` instead of hunting for a typo.
func TestTriageOnASeedWithNoScansBlamesTheMissingHistory(t *testing.T) {
	withTempStore(t)
	withFakeResolver(t, scriptedResolver{})

	_, _, err := run(t, "triage", "never-scanned.com", "xample.com", "--status", "benign")
	if err == nil {
		t.Fatal("triage on an unscanned seed succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "no recorded scans") {
		t.Errorf("error = %q, want it to name the missing history rather than the candidate", err)
	}
	if !strings.Contains(err.Error(), "yatt scan never-scanned.com") {
		t.Errorf("error = %q, want it to point at the command that fixes it", err)
	}

	// A mistyped seed usually looks exactly like an unscanned one, so the
	// suggestion still rides along when there is a seed to suggest.
	mustRun(t, "scan", "unicrons.cloud", "--technique", "omission")

	_, _, err = run(t, "triage", "unicorns.cloud", "nicrons.cloud", "--status", "benign")
	if err == nil {
		t.Fatal("triage on a mistyped seed succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "no recorded scans") {
		t.Errorf("error = %q, want it to lead with the missing history", err)
	}
	if !strings.Contains(err.Error(), "yatt triage unicrons.cloud nicrons.cloud") {
		t.Errorf("error = %q, want it to name the seed that does have the candidate", err)
	}
}

// The escape hatch: pre-recording a verdict before the first scan, or judging a
// candidate a narrower technique set did not produce.
func TestTriageForceBypassesTheRecordedCheck(t *testing.T) {
	withTempStore(t)
	withFakeResolver(t, scriptedResolver{})

	mustRun(t, "triage", "never-scanned.com", "future-candidate.com", "--status", "watchlist", "--force")

	entries := decodeTriage(t, mustRun(t, "triage", "list", "never-scanned.com", "--output", "json"))
	if len(entries) != 1 || entries[0].Candidate != "future-candidate.com" {
		t.Errorf("--force stored %+v, want the pre-recorded verdict", entries)
	}
}

// The seed's own row is a candidate of its own scan, so a verdict on the domain
// being protected — "we own this, obviously" — must keep working.
func TestTriageAcceptsTheSeedItself(t *testing.T) {
	withTempStore(t)
	withFakeResolver(t, scriptedResolver{registered: map[string]bool{"example.com": true}})

	mustRun(t, "scan", "example.com", "--technique", "omission")
	mustRun(t, "triage", "example.com", "example.com", "--status", "owned", "--note", "the real one")

	findings := decodeFindings(t, mustRun(t, "scan", "example.com", "--output", "json", "--technique", "omission"))
	if got := findingFor(t, findings, "example.com").Triage; got != triage.StatusOwned {
		t.Errorf("seed row triage = %q, want %q", got, triage.StatusOwned)
	}
}

// `yatt diff` renders the same columns as `yatt scan`, so it carries the same
// verdicts.
func TestDiffCarriesTriageStatus(t *testing.T) {
	withTempStore(t)

	registered := map[string]bool{}
	withFakeResolver(t, scriptedResolver{registered: registered})

	mustRun(t, "scan", "example.com", "--technique", "omission")
	registered["xample.com"] = true
	mustRun(t, "scan", "example.com", "--technique", "omission")
	mustRun(t, "triage", "example.com", "xample.com", "--status", "malicious")

	withForbiddenResolver(t)

	changes := decodeFindings(t, mustRun(t, "diff", "example.com", "--output", "json"))
	if len(changes) != 2 {
		t.Fatalf("diff reported %d rows, want the seed plus 1 change", len(changes))
	}
	if changes[1].Triage != triage.StatusMalicious {
		t.Errorf("diff triage = %q, want %q", changes[1].Triage, triage.StatusMalicious)
	}
}

// decodeTriage parses the JSON output of a triage command.
func decodeTriage(t *testing.T, stdout string) []store.Triage {
	t.Helper()

	var entries []store.Triage
	if err := json.Unmarshal([]byte(stdout), &entries); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, stdout)
	}
	return entries
}

func candidateNames(findings []scan.Finding) []string {
	out := make([]string, 0, len(findings))
	for _, f := range findings {
		out = append(out, f.Candidate)
	}
	return out
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
