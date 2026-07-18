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
	"strings"
	"time"
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
	Addresses   []string
	NS          []string
	MX          []string
	Rcode       string
	Error       string
}

// Store records scans and reads back the history of a seed.
type Store interface {
	// CreateScan records a new run and returns its identifier.
	CreateScan(ctx context.Context, seed string, profile string) (int64, error)
	// SaveFindings attaches findings to a scan.
	SaveFindings(ctx context.Context, scanID int64, findings []Finding) error
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
	// Close releases the underlying database handle.
	Close() error
}

// NormalizeSeed is the canonical form a seed is keyed by, so "Example.COM." and
// "example.com" share one history.
func NormalizeSeed(seed string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(seed)), ".")
}

// DefaultPath returns the database location used when --db is not given.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "yatt", "yatt.db"), nil
}
