// Package store persists scans and their findings.
//
// Everything goes through the Store interface over database/sql. That seam is
// the point: the local SQLite database is a phase-1 convenience, but the triage
// history it accumulates is the data that has to survive the eventual Postgres
// cutover, so no caller is allowed to depend on SQLite specifics.
package store

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/andoniaf/yatt/internal/triage"
	"github.com/andoniaf/yatt/pkg/engine"
)

// Scan is one recorded run against a seed domain.
type Scan struct {
	ID        int64     `json:"id"`
	Seed      string    `json:"seed"`
	Profile   string    `json:"profile,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	// Candidates and Registered are populated by ListScans so a history listing
	// costs one query rather than one per scan.
	Candidates int `json:"candidates"`
	Registered int `json:"registered"`
}

// Finding is the persisted form of one candidate domain.
//
// It deliberately duplicates the shape of scan.Finding rather than reusing it.
// scan is the domain package and it persists through this one, so importing it
// here would close an import cycle; the mapping between the two lives in scan,
// at the boundary.
type Finding struct {
	Candidate   string
	Registrable string
	Technique   string
	Registered  bool
	HasNS       bool
	HasA        bool
	HasMX       bool
	Wildcard    bool
	Addresses   []string
	NS          []string
	MX          []string
	Rcode       string
	Error       string
}

// Triage is an analyst's recorded verdict on one candidate of one seed.
//
// Unlike Finding it is not scoped to a scan: the same row is read by every
// future scan of the seed, which is what makes a verdict persistent.
type Triage struct {
	Seed      string        `json:"seed"`
	Candidate string        `json:"candidate"`
	Status    triage.Status `json:"status"`
	Note      string        `json:"note,omitempty"`
	UpdatedAt time.Time     `json:"updated_at"`
}

// CandidateOrigin says where a candidate name has actually been recorded.
//
// It exists so a command can tell three situations apart that all look like
// "this candidate is unknown": the seed has never been scanned, the candidate
// belongs to a different seed, and the candidate belongs nowhere at all. Only
// the middle one can be turned into a corrected command line, so the three have
// to be distinguishable before the error message is written.
type CandidateOrigin struct {
	// Recorded reports whether the candidate appears in any recorded scan of
	// the seed asked about, including as the seed's own row.
	Recorded bool
	// SeedScanned reports whether the seed has any recorded scan at all.
	SeedScanned bool
	// OtherSeeds names seeds that did record this candidate, most recently
	// scanned first and capped by the caller's limit. It is only populated when
	// Recorded is false, since it exists to answer "did you mean this seed?".
	OtherSeeds []string
}

// Store records scans and reads back the history of a seed.
type Store interface {
	// RecordScan records a new run together with its findings, atomically,
	// and returns the scan's identifier. One transaction is the contract, not
	// an implementation detail: a scan row committed without its findings
	// would read as an empty scan and become the baseline the next run diffs
	// against, silently resetting every candidate to "new".
	RecordScan(ctx context.Context, seed, profile string, findings []Finding) (int64, error)
	// LastScan returns the most recent scan of seed and its findings. It
	// returns a nil scan and no error when the seed has never been scanned, so
	// a first run is an ordinary outcome rather than an error path.
	LastScan(ctx context.Context, seed string) (*Scan, []Finding, error)
	// ScanAt returns the nth-most-recent scan of seed and its findings, where
	// offset 0 is the last scan. It returns a nil scan when that far back does
	// not exist.
	ScanAt(ctx context.Context, seed string, offset int) (*Scan, []Finding, error)
	// ListScans returns every scan of seed, most recent first.
	ListScans(ctx context.Context, seed string) ([]Scan, error)
	// LookupCandidate reports whether candidate has ever been recorded under
	// seed, and if not, which other seeds did record it — at most limit of them,
	// so the answer stays bounded no matter how many seeds share a candidate.
	//
	// It answers from what scans actually stored rather than by re-deriving the
	// permutation set, because the permutation set moves with the technique and
	// profile flags: a candidate produced by yesterday's run is a real part of
	// this seed's history even if today's narrower flags would not emit it.
	LookupCandidate(ctx context.Context, seed, candidate string, limit int) (CandidateOrigin, error)
	// GetTriage returns every recorded verdict for seed, keyed by candidate.
	// Candidates nobody has judged are simply absent rather than present with a
	// "new" status, so the map size is the number of decisions actually made.
	GetTriage(ctx context.Context, seed string) (map[string]Triage, error)
	// SetTriage records or replaces the verdict on one candidate and returns the
	// stored row.
	SetTriage(ctx context.Context, seed, candidate string, status triage.Status, note string) (Triage, error)
	// ListTriage returns every recorded verdict for seed, most recently updated
	// first.
	ListTriage(ctx context.Context, seed string) ([]Triage, error)
	// Close releases the underlying database handle.
	Close() error
}

// NormalizeSeed is the canonical form a seed is keyed by, so "Example.COM." and
// "example.com" share one history. The fold goes through engine.NormalizeDomain
// so IDN spellings converge too: the scanner stores and queries DNS wire
// forms, and without the fold "münchen.de" and "xn--mnchen-3ya.de" — the same
// domain, and the second is what the scan report prints — would key two
// disjoint histories.
func NormalizeSeed(seed string) string {
	return engine.NormalizeDomain(seed)
}

// NormalizeCandidate is the canonical form a candidate is keyed by.
//
// It is deliberately the same normalization as NormalizeSeed — both are domain
// names — so a verdict typed as "XAMPLE.COM." (or as a Unicode homoglyph
// copied from a browser's URL bar) lands on the row the scanner wrote as
// "xample.com" (or its "xn--" wire form) instead of creating a second,
// invisible one.
func NormalizeCandidate(candidate string) string {
	return NormalizeSeed(candidate)
}

// DefaultPath returns the database location used when --db is not given.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "yatt", "yatt.db"), nil
}
