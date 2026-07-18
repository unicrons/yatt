package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	// modernc.org/sqlite is the pure-Go driver: no cgo, so the binary stays
	// statically linkable and cross-compiles without a C toolchain.
	_ "modernc.org/sqlite"

	"github.com/andoniaf/yatt/internal/store/migrations"
)

// timeFormat is the ISO-8601 convention every timestamp column uses. SQLite has
// no native timestamp type and Postgres does, so pinning one textual format
// keeps the two schemas readable and comparable.
const timeFormat = time.RFC3339Nano

// listSeparator joins the record slices stored in a single TEXT column. DNS
// names and IP addresses never contain a comma, so the encoding is lossless and
// stays legible under `sqlite3 <db> .dump`.
const listSeparator = ","

// SQLite is the Store backed by a local SQLite database file.
type SQLite struct {
	db   *sql.DB
	path string
}

// compile-time check that the implementation satisfies the seam.
var _ Store = (*SQLite)(nil)

// Open opens (creating if needed) the SQLite database at path and brings its
// schema up to date.
//
// WAL plus a busy timeout is the standard mitigation for SQLite's single-writer
// model: readers no longer block on the writer, and a concurrent writer waits
// instead of failing immediately with SQLITE_BUSY.
func Open(path string) (*SQLite, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("store: empty database path")
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("store: creating %s: %w", dir, err)
		}
	}

	dsn := "file:" + path + "?_pragma=journal_mode(wal)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: opening %s: %w", path, err)
	}
	// SQLite tolerates exactly one writer; letting database/sql hand out more
	// connections only converts serialization into lock contention.
	db.SetMaxOpenConns(1)

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: opening %s: %w", path, err)
	}
	if err := migrations.Up(db, "sqlite"); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &SQLite{db: db, path: path}, nil
}

// Path returns the database file backing this store.
func (s *SQLite) Path() string { return s.path }

// Close implements Store.
func (s *SQLite) Close() error { return s.db.Close() }

// CreateScan implements Store.
func (s *SQLite) CreateScan(ctx context.Context, seed string, profile string) (int64, error) {
	seed = NormalizeSeed(seed)
	if seed == "" {
		return 0, errors.New("store: empty seed")
	}

	var id int64
	// RETURNING works on SQLite 3.35+ and has always worked on Postgres, so the
	// insert stays identical across both engines.
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO scans (seed, profile, created_at) VALUES (?, ?, ?) RETURNING id`,
		seed, profile, time.Now().UTC().Format(timeFormat),
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("store: creating scan for %s: %w", seed, err)
	}
	return id, nil
}

// SaveFindings implements Store.
//
// All findings of a scan are written in one transaction, so a scan row is never
// left referencing a half-written result set.
func (s *SQLite) SaveFindings(ctx context.Context, scanID int64, findings []Finding) error {
	if len(findings) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: saving findings: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO findings (
			scan_id, candidate, registrable, technique,
			registered, has_ns, has_a, has_mx,
			addresses, ns, mx, rcode, error
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("store: saving findings: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for _, f := range findings {
		if _, err := stmt.ExecContext(ctx,
			scanID, f.Candidate, f.Registrable, f.Technique,
			boolToInt(f.Registered), boolToInt(f.HasNS), boolToInt(f.HasA), boolToInt(f.HasMX),
			joinList(f.Addresses), joinList(f.NS), joinList(f.MX), f.Rcode, f.Error,
		); err != nil {
			return fmt.Errorf("store: saving finding %s: %w", f.Candidate, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: saving findings: %w", err)
	}
	return nil
}

// LastScan implements Store.
func (s *SQLite) LastScan(ctx context.Context, seed string) (*Scan, []Finding, error) {
	return s.ScanAt(ctx, seed, 0)
}

// ScanAt implements Store.
func (s *SQLite) ScanAt(ctx context.Context, seed string, offset int) (*Scan, []Finding, error) {
	if offset < 0 {
		return nil, nil, fmt.Errorf("store: negative scan offset %d", offset)
	}
	seed = NormalizeSeed(seed)

	var (
		scan      Scan
		createdAt string
	)
	// Ordering by created_at then id keeps the sequence stable when two scans of
	// the same seed land inside the same timestamp resolution.
	err := s.db.QueryRowContext(ctx, `
		SELECT id, seed, profile, created_at
		FROM scans
		WHERE seed = ?
		ORDER BY created_at DESC, id DESC
		LIMIT 1 OFFSET ?`, seed, offset,
	).Scan(&scan.ID, &scan.Seed, &scan.Profile, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		// Never having scanned this seed is an ordinary first run, not a
		// failure, so the caller gets a nil scan rather than an error to
		// special-case.
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("store: reading last scan of %s: %w", seed, err)
	}
	if scan.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, nil, err
	}

	findings, err := s.findings(ctx, scan.ID)
	if err != nil {
		return nil, nil, err
	}
	scan.Candidates = len(findings)
	for _, f := range findings {
		if f.Registered {
			scan.Registered++
		}
	}
	return &scan, findings, nil
}

// ListScans implements Store.
func (s *SQLite) ListScans(ctx context.Context, seed string) ([]Scan, error) {
	seed = NormalizeSeed(seed)

	rows, err := s.db.QueryContext(ctx, `
		SELECT s.id, s.seed, s.profile, s.created_at,
		       COUNT(f.id),
		       COALESCE(SUM(f.registered), 0)
		FROM scans s
		LEFT JOIN findings f ON f.scan_id = s.id
		WHERE s.seed = ?
		GROUP BY s.id, s.seed, s.profile, s.created_at
		ORDER BY s.created_at DESC, s.id DESC`, seed)
	if err != nil {
		return nil, fmt.Errorf("store: listing scans of %s: %w", seed, err)
	}
	defer func() { _ = rows.Close() }()

	var scans []Scan
	for rows.Next() {
		var (
			scan      Scan
			createdAt string
		)
		if err := rows.Scan(&scan.ID, &scan.Seed, &scan.Profile, &createdAt,
			&scan.Candidates, &scan.Registered); err != nil {
			return nil, fmt.Errorf("store: listing scans of %s: %w", seed, err)
		}
		if scan.CreatedAt, err = parseTime(createdAt); err != nil {
			return nil, err
		}
		scans = append(scans, scan)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: listing scans of %s: %w", seed, err)
	}
	return scans, nil
}

// findings reads every finding of one scan, in insertion order so a stored scan
// reads back in the deterministic order the engine produced it.
func (s *SQLite) findings(ctx context.Context, scanID int64) ([]Finding, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT candidate, registrable, technique,
		       registered, has_ns, has_a, has_mx,
		       addresses, ns, mx, rcode, error
		FROM findings
		WHERE scan_id = ?
		ORDER BY id`, scanID)
	if err != nil {
		return nil, fmt.Errorf("store: reading findings of scan %d: %w", scanID, err)
	}
	defer func() { _ = rows.Close() }()

	var findings []Finding
	for rows.Next() {
		var (
			f                                   Finding
			registered, hasNS, hasA, hasMX      int
			addresses, nameservers, mailservers string
		)
		if err := rows.Scan(
			&f.Candidate, &f.Registrable, &f.Technique,
			&registered, &hasNS, &hasA, &hasMX,
			&addresses, &nameservers, &mailservers, &f.Rcode, &f.Error,
		); err != nil {
			return nil, fmt.Errorf("store: reading findings of scan %d: %w", scanID, err)
		}
		f.Registered = registered != 0
		f.HasNS = hasNS != 0
		f.HasA = hasA != 0
		f.HasMX = hasMX != 0
		f.Addresses = splitList(addresses)
		f.NS = splitList(nameservers)
		f.MX = splitList(mailservers)
		findings = append(findings, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: reading findings of scan %d: %w", scanID, err)
	}
	return findings, nil
}

// boolToInt encodes a boolean as the 1/0 literal both engines accept, since
// SQLite has no boolean type.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func joinList(values []string) string {
	return strings.Join(values, listSeparator)
}

func splitList(value string) []string {
	if value == "" {
		return nil
	}
	return strings.Split(value, listSeparator)
}

func parseTime(value string) (time.Time, error) {
	t, err := time.Parse(timeFormat, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("store: unparseable timestamp %q: %w", value, err)
	}
	return t.UTC(), nil
}
