package store_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/andoniaf/yatt/internal/store"
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

func TestOpenIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "yatt.db")

	first, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := first.CreateScan(context.Background(), "example.com", ""); err != nil {
		t.Fatalf("CreateScan: %v", err)
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

	id, err := s.CreateScan(ctx, "example.com", "quick")
	if err != nil {
		t.Fatalf("CreateScan: %v", err)
	}
	if err := s.SaveFindings(ctx, id, sampleFindings()); err != nil {
		t.Fatalf("SaveFindings: %v", err)
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

	first, err := s.CreateScan(ctx, "example.com", "")
	if err != nil {
		t.Fatalf("CreateScan: %v", err)
	}
	if err := s.SaveFindings(ctx, first, sampleFindings()); err != nil {
		t.Fatalf("SaveFindings: %v", err)
	}

	second, err := s.CreateScan(ctx, "example.com", "")
	if err != nil {
		t.Fatalf("CreateScan: %v", err)
	}
	newer := sampleFindings()[:1]
	if err := s.SaveFindings(ctx, second, newer); err != nil {
		t.Fatalf("SaveFindings: %v", err)
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
		id, err := s.CreateScan(ctx, "example.com", "quick")
		if err != nil {
			t.Fatalf("CreateScan: %v", err)
		}
		if err := s.SaveFindings(ctx, id, sampleFindings()); err != nil {
			t.Fatalf("SaveFindings: %v", err)
		}
		ids = append(ids, id)
	}
	if _, err := s.CreateScan(ctx, "other.com", ""); err != nil {
		t.Fatalf("CreateScan: %v", err)
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

	if _, err := s.CreateScan(ctx, "example.com", ""); err != nil {
		t.Fatalf("CreateScan: %v", err)
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

	if _, err := s.CreateScan(ctx, "  Example.COM.  ", ""); err != nil {
		t.Fatalf("CreateScan: %v", err)
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

func TestCreateScanRejectsAnEmptySeed(t *testing.T) {
	if _, err := open(t).CreateScan(context.Background(), "   ", ""); err == nil {
		t.Error("CreateScan(\"\") succeeded, want an error")
	}
}

func TestSaveFindingsRejectsAnUnknownScan(t *testing.T) {
	// The foreign key must be enforced, otherwise findings can outlive — or
	// precede — the scan that owns them.
	if err := open(t).SaveFindings(context.Background(), 999, sampleFindings()); err == nil {
		t.Error("SaveFindings against an unknown scan succeeded, want an error")
	}
}
