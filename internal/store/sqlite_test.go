package store_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/andoniaf/yatt/internal/store"
	"github.com/andoniaf/yatt/internal/triage"
	"github.com/andoniaf/yatt/pkg/engine"
)

// open returns a store backed by a fresh temp-file database. The repository is
// exercised against real SQLite rather than a mock, because the behaviour worth
// testing here — migrations, the portable SQL subset, round-tripping — is
// exactly what a mock would assume away.
func open(t *testing.T) *store.SQLite {
	t.Helper()

	s, err := store.Open(filepath.Join(t.TempDir(), "yatt.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func sampleFindings() []store.Finding {
	return []store.Finding{
		{
			Candidate:   "xample.com",
			Registrable: "xample.com",
			Technique:   "omission",
			Registered:  true,
			HasNS:       true,
			HasA:        true,
			Addresses:   []string{"192.0.2.1", "192.0.2.2"},
			NS:          []string{"ns1.example.net", "ns2.example.net"},
			Rcode:       "NOERROR",
		},
		{
			Candidate:   "eample.com",
			Registrable: "eample.com",
			Technique:   "omission",
			Rcode:       "NXDOMAIN",
		},
	}
}

// The seed's own row is stored like a candidate so it takes part in the diff,
// but both count paths — the per-scan one and the aggregate behind a history
// listing — must leave it out of "candidates" and "registered".
func TestScanCountsExcludeTheSeedRow(t *testing.T) {
	ctx := context.Background()
	s := open(t)

	findings := append([]store.Finding{{
		Candidate:   "example.com",
		Registrable: "example.com",
		Technique:   engine.TechniqueOriginal,
		Registered:  true,
		HasNS:       true,
		Rcode:       "NOERROR",
	}}, sampleFindings()...)
	if _, err := s.RecordScan(ctx, "example.com", "", findings); err != nil {
		t.Fatalf("RecordScan: %v", err)
	}

	scan, stored, err := s.LastScan(ctx, "example.com")
	if err != nil {
		t.Fatalf("LastScan: %v", err)
	}
	if scan.Candidates != 2 || scan.Registered != 1 {
		t.Errorf("LastScan counts = %d/%d, want 2 candidates and 1 registered",
			scan.Candidates, scan.Registered)
	}
	// It is excluded from the counts, not from the data: the diff needs it.
	if len(stored) != 3 {
		t.Errorf("read back %d findings, want the seed row plus 2 candidates", len(stored))
	}

	scans, err := s.ListScans(ctx, "example.com")
	if err != nil {
		t.Fatalf("ListScans: %v", err)
	}
	if len(scans) != 1 || scans[0].Candidates != 2 || scans[0].Registered != 1 {
		t.Errorf("ListScans counts = %d/%d, want 2 candidates and 1 registered",
			scans[0].Candidates, scans[0].Registered)
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "yatt.db")

	first, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := first.RecordScan(context.Background(), "example.com", "", nil); err != nil {
		t.Fatalf("RecordScan: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Re-opening must re-run migrations harmlessly and preserve prior data.
	second, err := store.Open(path)
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	defer func() { _ = second.Close() }()

	scans, err := second.ListScans(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("ListScans: %v", err)
	}
	if len(scans) != 1 {
		t.Errorf("got %d scans after reopening, want 1", len(scans))
	}
}

func TestSaveAndReadBackFindings(t *testing.T) {
	ctx := context.Background()
	s := open(t)

	id, err := s.RecordScan(ctx, "example.com", "quick", sampleFindings())
	if err != nil {
		t.Fatalf("RecordScan: %v", err)
	}

	scan, findings, err := s.LastScan(ctx, "example.com")
	if err != nil {
		t.Fatalf("LastScan: %v", err)
	}
	if scan == nil {
		t.Fatal("LastScan returned no scan")
	}
	if scan.ID != id || scan.Seed != "example.com" || scan.Profile != "quick" {
		t.Errorf("scan = %+v, want id %d, seed example.com, profile quick", scan, id)
	}
	if scan.Candidates != 2 || scan.Registered != 1 {
		t.Errorf("scan counts = %d/%d, want 2 candidates and 1 registered", scan.Candidates, scan.Registered)
	}
	if scan.CreatedAt.IsZero() || time.Since(scan.CreatedAt) > time.Minute {
		t.Errorf("created_at = %v, want a recent timestamp", scan.CreatedAt)
	}

	if len(findings) != 2 {
		t.Fatalf("got %d findings, want 2", len(findings))
	}
	// Findings must read back in the order they were written, so a stored scan
	// diffs against a live one without any re-sorting.
	if findings[0].Candidate != "xample.com" || findings[1].Candidate != "eample.com" {
		t.Errorf("findings out of insertion order: %q, %q", findings[0].Candidate, findings[1].Candidate)
	}

	got, want := findings[0], sampleFindings()[0]
	if got.Registered != want.Registered || got.HasNS != want.HasNS || got.HasA != want.HasA || got.HasMX != want.HasMX {
		t.Errorf("signals = %+v, want %+v", got, want)
	}
	if len(got.Addresses) != 2 || got.Addresses[0] != "192.0.2.1" || got.Addresses[1] != "192.0.2.2" {
		t.Errorf("addresses = %v, want the two stored addresses", got.Addresses)
	}
	if len(got.NS) != 2 {
		t.Errorf("ns = %v, want two nameservers", got.NS)
	}
	if got.Rcode != "NOERROR" {
		t.Errorf("rcode = %q, want NOERROR", got.Rcode)
	}

	// An empty record slice must round-trip as empty, not as one empty string.
	if len(findings[1].Addresses) != 0 || len(findings[1].NS) != 0 || len(findings[1].MX) != 0 {
		t.Errorf("unregistered finding = %+v, want no records", findings[1])
	}
}

func TestLastScanOfUnknownSeedIsNotAnError(t *testing.T) {
	scan, findings, err := open(t).LastScan(context.Background(), "never-scanned.com")
	if err != nil {
		t.Fatalf("LastScan: %v", err)
	}
	if scan != nil || findings != nil {
		t.Errorf("LastScan = %v, %v, want nil, nil for an unscanned seed", scan, findings)
	}
}

func TestLastScanReturnsTheMostRecentScan(t *testing.T) {
	ctx := context.Background()
	s := open(t)

	first, err := s.RecordScan(ctx, "example.com", "", sampleFindings())
	if err != nil {
		t.Fatalf("RecordScan: %v", err)
	}

	second, err := s.RecordScan(ctx, "example.com", "", sampleFindings()[:1])
	if err != nil {
		t.Fatalf("RecordScan: %v", err)
	}

	last, findings, err := s.LastScan(ctx, "example.com")
	if err != nil {
		t.Fatalf("LastScan: %v", err)
	}
	if last.ID != second {
		t.Errorf("LastScan id = %d, want %d", last.ID, second)
	}
	if len(findings) != 1 {
		t.Errorf("got %d findings, want 1", len(findings))
	}

	// Offset 1 is the scan before it — the one a diff compares against.
	previous, previousFindings, err := s.ScanAt(ctx, "example.com", 1)
	if err != nil {
		t.Fatalf("ScanAt: %v", err)
	}
	if previous.ID != first {
		t.Errorf("ScanAt(1) id = %d, want %d", previous.ID, first)
	}
	if len(previousFindings) != 2 {
		t.Errorf("got %d previous findings, want 2", len(previousFindings))
	}

	// Reaching past the beginning of the history is an ordinary "nothing to
	// compare against", not an error.
	beyond, _, err := s.ScanAt(ctx, "example.com", 5)
	if err != nil {
		t.Fatalf("ScanAt: %v", err)
	}
	if beyond != nil {
		t.Errorf("ScanAt(5) = %+v, want nil", beyond)
	}
}

func TestListScansIsMostRecentFirstAndSeedScoped(t *testing.T) {
	ctx := context.Background()
	s := open(t)

	var ids []int64
	for range 3 {
		id, err := s.RecordScan(ctx, "example.com", "quick", sampleFindings())
		if err != nil {
			t.Fatalf("RecordScan: %v", err)
		}
		ids = append(ids, id)
	}
	if _, err := s.RecordScan(ctx, "other.com", "", nil); err != nil {
		t.Fatalf("RecordScan: %v", err)
	}

	scans, err := s.ListScans(ctx, "example.com")
	if err != nil {
		t.Fatalf("ListScans: %v", err)
	}
	if len(scans) != 3 {
		t.Fatalf("got %d scans, want 3 — another seed's scans leaked in", len(scans))
	}
	for i, want := range []int64{ids[2], ids[1], ids[0]} {
		if scans[i].ID != want {
			t.Errorf("scans[%d].ID = %d, want %d", i, scans[i].ID, want)
		}
	}
	if scans[0].Candidates != 2 || scans[0].Registered != 1 {
		t.Errorf("counts = %d/%d, want 2 candidates and 1 registered",
			scans[0].Candidates, scans[0].Registered)
	}
}

func TestListScansCountsAScanWithNoFindings(t *testing.T) {
	ctx := context.Background()
	s := open(t)

	if _, err := s.RecordScan(ctx, "example.com", "", nil); err != nil {
		t.Fatalf("RecordScan: %v", err)
	}

	scans, err := s.ListScans(ctx, "example.com")
	if err != nil {
		t.Fatalf("ListScans: %v", err)
	}
	// The LEFT JOIN must still yield the scan row, with zeroed counts.
	if len(scans) != 1 || scans[0].Candidates != 0 || scans[0].Registered != 0 {
		t.Errorf("ListScans = %+v, want one scan with zero counts", scans)
	}
}

func TestSeedsAreNormalized(t *testing.T) {
	ctx := context.Background()
	s := open(t)

	if _, err := s.RecordScan(ctx, "  Example.COM.  ", "", nil); err != nil {
		t.Fatalf("RecordScan: %v", err)
	}

	// A seed typed differently must not fork the history it is keyed by.
	scans, err := s.ListScans(ctx, "example.com")
	if err != nil {
		t.Fatalf("ListScans: %v", err)
	}
	if len(scans) != 1 {
		t.Errorf("got %d scans, want 1 — seed normalization forked the history", len(scans))
	}
}

func TestRecordScanRejectsAnEmptySeed(t *testing.T) {
	if _, err := open(t).RecordScan(context.Background(), "   ", "", nil); err == nil {
		t.Error("RecordScan(\"\") succeeded, want an error")
	}
}

// A failed persist must leave nothing behind: a scan row committed without its
// findings would read as an empty scan and become the baseline the next run
// diffs against.
func TestRecordScanIsAtomic(t *testing.T) {
	ctx := context.Background()
	s := open(t)

	// A duplicate candidate violates the (scan_id, candidate) unique index
	// partway through the findings, after the scan row was already inserted
	// inside the transaction.
	duplicated := append(sampleFindings(), sampleFindings()...)
	if _, err := s.RecordScan(ctx, "example.com", "", duplicated); err == nil {
		t.Fatal("RecordScan with duplicate candidates succeeded, want an error")
	}

	scans, err := s.ListScans(ctx, "example.com")
	if err != nil {
		t.Fatalf("ListScans: %v", err)
	}
	if len(scans) != 0 {
		t.Errorf("got %d scans after a failed RecordScan, want 0 — the scan row escaped the transaction", len(scans))
	}
}

// The wildcard flag must survive persistence: a diff or gone row is rebuilt
// from the store, and losing the flag there would re-present catch-all noise
// as real registered look-alikes.
func TestFindingsRoundTripTheWildcardFlag(t *testing.T) {
	ctx := context.Background()
	s := open(t)

	findings := sampleFindings()
	findings[0].Wildcard = true
	if _, err := s.RecordScan(ctx, "example.com", "", findings); err != nil {
		t.Fatalf("RecordScan: %v", err)
	}

	_, stored, err := s.LastScan(ctx, "example.com")
	if err != nil {
		t.Fatalf("LastScan: %v", err)
	}
	if len(stored) != 2 || !stored[0].Wildcard || stored[1].Wildcard {
		t.Errorf("stored wildcard flags = %+v, want only the first finding flagged", stored)
	}
}

// record is a scan of seed containing candidates, for the lookup tests.
func record(t *testing.T, s *store.SQLite, seed string, candidates ...string) {
	t.Helper()

	findings := make([]store.Finding, 0, len(candidates))
	for _, candidate := range candidates {
		findings = append(findings, store.Finding{Candidate: candidate, Registrable: candidate})
	}
	if _, err := s.RecordScan(context.Background(), seed, "", findings); err != nil {
		t.Fatalf("RecordScan: %v", err)
	}
}

func TestLookupCandidate(t *testing.T) {
	ctx := context.Background()
	s := open(t)

	record(t, s, "example.com", "example.com", "xample.com", "eample.com")
	record(t, s, "unicrons.cloud", "unicrons.cloud", "nicrons.cloud")

	tests := []struct {
		name        string
		seed        string
		candidate   string
		recorded    bool
		seedScanned bool
		otherSeeds  []string
	}{
		{
			name:        "recorded under the seed",
			seed:        "example.com",
			candidate:   "xample.com",
			recorded:    true,
			seedScanned: true,
		},
		{
			// The seed is stored like one of its own candidates so it takes part
			// in the diff, which also makes it a legitimate triage target.
			name:        "the seed's own row",
			seed:        "example.com",
			candidate:   "example.com",
			recorded:    true,
			seedScanned: true,
		},
		{
			name:        "recorded under a different seed",
			seed:        "example.com",
			candidate:   "nicrons.cloud",
			seedScanned: true,
			otherSeeds:  []string{"unicrons.cloud"},
		},
		{
			name:        "recorded nowhere",
			seed:        "example.com",
			candidate:   "unrelated.test",
			seedScanned: true,
		},
		{
			// An unscanned seed is reported as such, and the alternatives are
			// still gathered: a caller that leads with "nothing was ever scanned
			// here" can still add "...and that name belongs to this other seed",
			// which is usually what a mistyped seed looks like.
			name:       "seed never scanned",
			seed:       "never-scanned.com",
			candidate:  "xample.com",
			otherSeeds: []string{"example.com"},
		},
		{
			name:        "normalizes both names",
			seed:        " Example.COM. ",
			candidate:   " XAMPLE.com. ",
			recorded:    true,
			seedScanned: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.LookupCandidate(ctx, tt.seed, tt.candidate, 0)
			if err != nil {
				t.Fatalf("LookupCandidate: %v", err)
			}
			if got.Recorded != tt.recorded || got.SeedScanned != tt.seedScanned {
				t.Errorf("LookupCandidate = %+v, want recorded=%v scanned=%v",
					got, tt.recorded, tt.seedScanned)
			}
			if len(got.OtherSeeds) != len(tt.otherSeeds) {
				t.Fatalf("other seeds = %v, want %v", got.OtherSeeds, tt.otherSeeds)
			}
			for i, want := range tt.otherSeeds {
				if got.OtherSeeds[i] != want {
					t.Errorf("other seeds[%d] = %q, want %q", i, got.OtherSeeds[i], want)
				}
			}
		})
	}
}

// The alternate seeds feed a "did you mean?" suggestion, so the answer has to
// stay short and lead with the seed the analyst most likely meant.
func TestLookupCandidateBoundsAndOrdersAlternateSeeds(t *testing.T) {
	ctx := context.Background()
	s := open(t)

	// A candidate shared by several seeds — plausible when one organisation
	// watches related brands. Scanned oldest first, so the last one recorded is
	// the most recent.
	for _, seed := range []string{"first.com", "second.com", "third.com", "fourth.com"} {
		record(t, s, seed, "shared.com")
		time.Sleep(time.Millisecond)
	}

	got, err := s.LookupCandidate(ctx, "example.com", "shared.com", 2)
	if err != nil {
		t.Fatalf("LookupCandidate: %v", err)
	}
	if len(got.OtherSeeds) != 2 {
		t.Fatalf("other seeds = %v, want the limit of 2 honoured", got.OtherSeeds)
	}
	if got.OtherSeeds[0] != "fourth.com" || got.OtherSeeds[1] != "third.com" {
		t.Errorf("other seeds = %v, want the most recently scanned first", got.OtherSeeds)
	}

	// A seed that recorded the candidate itself is never offered as an
	// alternative to itself.
	own, err := s.LookupCandidate(ctx, "third.com", "shared.com", 0)
	if err != nil {
		t.Fatalf("LookupCandidate: %v", err)
	}
	if !own.Recorded || len(own.OtherSeeds) != 0 {
		t.Errorf("LookupCandidate = %+v, want it recorded with no alternatives", own)
	}
}

func TestTriageOfAnUntriagedSeedIsEmpty(t *testing.T) {
	ctx := context.Background()
	s := open(t)

	verdicts, err := s.GetTriage(ctx, "never-triaged.com")
	if err != nil {
		t.Fatalf("GetTriage: %v", err)
	}
	if len(verdicts) != 0 {
		t.Errorf("GetTriage = %v, want no verdicts", verdicts)
	}

	entries, err := s.ListTriage(ctx, "never-triaged.com")
	if err != nil {
		t.Fatalf("ListTriage: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("ListTriage = %v, want no verdicts", entries)
	}
}

func TestSetTriageStoresAndReadsBack(t *testing.T) {
	ctx := context.Background()
	s := open(t)

	stored, err := s.SetTriage(ctx, "example.com", "xample.com", triage.StatusOwned, "defensive registration")
	if err != nil {
		t.Fatalf("SetTriage: %v", err)
	}
	if stored.Seed != "example.com" || stored.Candidate != "xample.com" {
		t.Errorf("stored = %+v, want it keyed by seed and candidate", stored)
	}
	if stored.Status != triage.StatusOwned || stored.Note != "defensive registration" {
		t.Errorf("stored = %+v, want the owned verdict and its note", stored)
	}
	if stored.UpdatedAt.IsZero() || time.Since(stored.UpdatedAt) > time.Minute {
		t.Errorf("updated_at = %v, want a recent timestamp", stored.UpdatedAt)
	}

	verdicts, err := s.GetTriage(ctx, "example.com")
	if err != nil {
		t.Fatalf("GetTriage: %v", err)
	}
	got, ok := verdicts["xample.com"]
	if !ok {
		t.Fatalf("GetTriage = %v, want an entry keyed by candidate", verdicts)
	}
	if got.Status != triage.StatusOwned || got.Note != "defensive registration" {
		t.Errorf("GetTriage entry = %+v, want the stored verdict", got)
	}
}

// The upsert is the whole reason for the unique constraint: re-triaging must
// overwrite the verdict, not accumulate a second, invisible one.
func TestSetTriageUpsertsRatherThanDuplicating(t *testing.T) {
	ctx := context.Background()
	s := open(t)

	if _, err := s.SetTriage(ctx, "example.com", "xample.com", triage.StatusSuspicious, "looks parked"); err != nil {
		t.Fatalf("SetTriage: %v", err)
	}
	if _, err := s.SetTriage(ctx, "example.com", "xample.com", triage.StatusMalicious, "serving a phishing page"); err != nil {
		t.Fatalf("SetTriage: %v", err)
	}

	entries, err := s.ListTriage(ctx, "example.com")
	if err != nil {
		t.Fatalf("ListTriage: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d triage rows, want 1 — the upsert duplicated instead of overwriting:\n%+v", len(entries), entries)
	}
	if entries[0].Status != triage.StatusMalicious || entries[0].Note != "serving a phishing page" {
		t.Errorf("entry = %+v, want the second verdict to have won", entries[0])
	}
}

// The moat: a verdict is keyed by seed and candidate, so it is still there
// after the scan that produced the candidate has been superseded.
func TestTriageSurvivesLaterScans(t *testing.T) {
	ctx := context.Background()
	s := open(t)

	if _, err := s.RecordScan(ctx, "example.com", "", sampleFindings()); err != nil {
		t.Fatalf("RecordScan: %v", err)
	}
	if _, err := s.SetTriage(ctx, "example.com", "xample.com", triage.StatusOwned, ""); err != nil {
		t.Fatalf("SetTriage: %v", err)
	}

	if _, err := s.RecordScan(ctx, "example.com", "", sampleFindings()); err != nil {
		t.Fatalf("RecordScan: %v", err)
	}

	verdicts, err := s.GetTriage(ctx, "example.com")
	if err != nil {
		t.Fatalf("GetTriage: %v", err)
	}
	if verdicts["xample.com"].Status != triage.StatusOwned {
		t.Errorf("verdict after a second scan = %+v, want it carried over as owned", verdicts["xample.com"])
	}
}

func TestTriageIsSeedScoped(t *testing.T) {
	ctx := context.Background()
	s := open(t)

	// The same candidate name under two different seeds is two independent
	// judgements: "owned" by one brand says nothing about another's exposure.
	if _, err := s.SetTriage(ctx, "example.com", "xample.com", triage.StatusOwned, ""); err != nil {
		t.Fatalf("SetTriage: %v", err)
	}
	if _, err := s.SetTriage(ctx, "other.com", "xample.com", triage.StatusMalicious, ""); err != nil {
		t.Fatalf("SetTriage: %v", err)
	}

	verdicts, err := s.GetTriage(ctx, "example.com")
	if err != nil {
		t.Fatalf("GetTriage: %v", err)
	}
	if len(verdicts) != 1 || verdicts["xample.com"].Status != triage.StatusOwned {
		t.Errorf("GetTriage = %v, want only this seed's owned verdict", verdicts)
	}
}

func TestSetTriageNormalizesSeedAndCandidate(t *testing.T) {
	ctx := context.Background()
	s := open(t)

	if _, err := s.SetTriage(ctx, " Example.COM. ", " XAMPLE.com. ", triage.StatusBenign, ""); err != nil {
		t.Fatalf("SetTriage: %v", err)
	}

	verdicts, err := s.GetTriage(ctx, "example.com")
	if err != nil {
		t.Fatalf("GetTriage: %v", err)
	}
	// A verdict typed in a different shape must land on the row the scanner
	// wrote, not create a parallel one nobody will ever see again.
	if _, ok := verdicts["xample.com"]; !ok {
		t.Errorf("GetTriage = %v, want the verdict keyed by the normalized candidate", verdicts)
	}
}

func TestSetTriageRejectsBadInput(t *testing.T) {
	tests := []struct {
		name      string
		seed      string
		candidate string
		status    triage.Status
	}{
		{name: "empty seed", seed: "  ", candidate: "xample.com", status: triage.StatusBenign},
		{name: "empty candidate", seed: "example.com", candidate: "  ", status: triage.StatusBenign},
		// The database is the last place a meaningless verdict can be caught,
		// and a status column is only worth reading if all of it is meaningful.
		{name: "unknown status", seed: "example.com", candidate: "xample.com", status: "bogus"},
		{name: "empty status", seed: "example.com", candidate: "xample.com", status: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := open(t).SetTriage(context.Background(), tt.seed, tt.candidate, tt.status, ""); err == nil {
				t.Error("SetTriage succeeded, want an error")
			}
		})
	}
}

func TestListTriageIsMostRecentlyUpdatedFirst(t *testing.T) {
	ctx := context.Background()
	s := open(t)

	for _, candidate := range []string{"aaa.com", "bbb.com", "ccc.com"} {
		if _, err := s.SetTriage(ctx, "example.com", candidate, triage.StatusBenign, ""); err != nil {
			t.Fatalf("SetTriage: %v", err)
		}
		// The timestamp has nanosecond resolution, but sleeping a hair keeps the
		// ordering unambiguous on a coarse clock.
		time.Sleep(time.Millisecond)
	}

	entries, err := s.ListTriage(ctx, "example.com")
	if err != nil {
		t.Fatalf("ListTriage: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}
	if entries[0].Candidate != "ccc.com" {
		t.Errorf("first entry = %q, want the most recently updated (ccc.com)", entries[0].Candidate)
	}
}
