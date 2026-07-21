package store

import (
	"context"
	"database/sql"
	"encoding/json"
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
	"github.com/andoniaf/yatt/internal/triage"
	"github.com/andoniaf/yatt/pkg/engine"
)

// timeFormat is the ISO-8601 convention every timestamp column uses. SQLite has
// no native timestamp type and Postgres does, so pinning one textual format
// keeps the two schemas readable and comparable.
//
// The fractional second is fixed-width rather than RFC3339Nano: Nano trims
// trailing zeros, and variable-width fractions do not sort as text ("...05.4Z"
// compares after "...05.42Z" because 'Z' > '2'). Every ORDER BY created_at in
// this file depends on the textual order being the chronological one.
const timeFormat = "2006-01-02T15:04:05.000000000Z07:00"

// listSeparator is the legacy encoding of the record slices stored in a
// single TEXT column: a bare comma join. It is still understood on read, but
// writes moved to JSON — DNS names can legally contain a comma (miekg/dns
// renders one literally), and the zones being scanned are by definition
// attacker-controlled, so a comma join would let a hostile NS/MX record
// split into fabricated entries on read-back.
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

// RecordScan implements Store.
//
// The scan row and every finding commit in one transaction, so a scan can
// never exist without its findings: a process killed or a disk filling up
// mid-persist rolls the whole run back instead of leaving an empty scan row
// as the next diff's baseline.
func (s *SQLite) RecordScan(ctx context.Context, seed, profile string, findings []Finding) (int64, error) {
	seed = NormalizeSeed(seed)
	if seed == "" {
		return 0, errors.New("store: empty seed")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("store: recording scan for %s: %w", seed, err)
	}
	defer func() { _ = tx.Rollback() }()

	var id int64
	// RETURNING works on SQLite 3.35+ and has always worked on Postgres, so the
	// insert stays identical across both engines.
	err = tx.QueryRowContext(ctx,
		`INSERT INTO scans (seed, profile, created_at) VALUES (?, ?, ?) RETURNING id`,
		seed, profile, time.Now().UTC().Format(timeFormat),
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("store: recording scan for %s: %w", seed, err)
	}

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO findings (
			scan_id, candidate, registrable, technique,
			registered, has_ns, has_a, has_mx, wildcard,
			addresses, ns, mx, rcode, error
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return 0, fmt.Errorf("store: recording scan for %s: %w", seed, err)
	}
	defer func() { _ = stmt.Close() }()

	for _, f := range findings {
		if _, err := stmt.ExecContext(ctx,
			id, f.Candidate, f.Registrable, f.Technique,
			boolToInt(f.Registered), boolToInt(f.HasNS), boolToInt(f.HasA), boolToInt(f.HasMX), boolToInt(f.Wildcard),
			joinList(f.Addresses), joinList(f.NS), joinList(f.MX), f.Rcode, f.Error,
		); err != nil {
			return 0, fmt.Errorf("store: recording finding %s: %w", f.Candidate, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("store: recording scan for %s: %w", seed, err)
	}
	return id, nil
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
	// The seed's own row is stored alongside the candidates so it takes part in
	// the diff, but it is not one of them and must not inflate the counts a
	// history listing reports.
	for _, f := range findings {
		if f.Technique == engine.TechniqueOriginal {
			continue
		}
		scan.Candidates++
		if f.Registered {
			scan.Registered++
		}
	}
	return &scan, findings, nil
}

// ListScans implements Store.
func (s *SQLite) ListScans(ctx context.Context, seed string) ([]Scan, error) {
	seed = NormalizeSeed(seed)

	// The seed's own row is excluded from both counts: it is stored like a
	// candidate so it takes part in the diff, but it is not one. CASE rather than
	// a FILTER clause keeps the aggregate inside the portable subset.
	rows, err := s.db.QueryContext(ctx, `
		SELECT s.id, s.seed, s.profile, s.created_at,
		       COALESCE(SUM(CASE WHEN f.id IS NOT NULL AND f.technique <> ? THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN f.registered = 1 AND f.technique <> ? THEN 1 ELSE 0 END), 0)
		FROM scans s
		LEFT JOIN findings f ON f.scan_id = s.id
		WHERE s.seed = ?
		GROUP BY s.id, s.seed, s.profile, s.created_at
		ORDER BY s.created_at DESC, s.id DESC`,
		engine.TechniqueOriginal, engine.TechniqueOriginal, seed)
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

// defaultCandidateSeedLimit bounds how many alternate seeds LookupCandidate
// reports. The result feeds a "did you mean?" error, and an error that lists
// twenty seeds has stopped being a suggestion, so a handful is both cheaper and
// more useful than the complete answer.
const defaultCandidateSeedLimit = 3

// LookupCandidate implements Store.
//
// Three small indexed probes rather than one clever statement, each skipped as
// soon as it cannot change the answer: the common case is a candidate that is
// recorded, and that costs exactly one row-existence check.
func (s *SQLite) LookupCandidate(ctx context.Context, seed, candidate string, limit int) (CandidateOrigin, error) {
	seed = NormalizeSeed(seed)
	candidate = NormalizeCandidate(candidate)
	if limit <= 0 {
		limit = defaultCandidateSeedLimit
	}

	// SELECT 1 ... LIMIT 1 is the portable existence check: it stops at the
	// first matching row and never materialises a finding, which is the point of
	// having this method rather than reading a whole scan back.
	recorded, err := s.exists(ctx, `
		SELECT 1
		FROM findings f
		JOIN scans s ON s.id = f.scan_id
		WHERE s.seed = ? AND f.candidate = ?
		LIMIT 1`, seed, candidate)
	if err != nil {
		return CandidateOrigin{}, fmt.Errorf("store: looking up %s under %s: %w", candidate, seed, err)
	}
	if recorded {
		// A recorded candidate needs no alternatives and implies the seed was
		// scanned, so the remaining two probes are pure cost.
		return CandidateOrigin{Recorded: true, SeedScanned: true}, nil
	}

	scanned, err := s.exists(ctx, `SELECT 1 FROM scans WHERE seed = ? LIMIT 1`, seed)
	if err != nil {
		return CandidateOrigin{}, fmt.Errorf("store: looking up scans of %s: %w", seed, err)
	}

	others, err := s.seedsRecording(ctx, candidate, seed, limit)
	if err != nil {
		return CandidateOrigin{}, err
	}
	return CandidateOrigin{SeedScanned: scanned, OtherSeeds: others}, nil
}

// seedsRecording names the other seeds whose scans contain candidate.
//
// Most recently scanned first: when an analyst confuses two seeds, the one they
// were just working on is overwhelmingly the one they meant.
func (s *SQLite) seedsRecording(ctx context.Context, candidate, excluding string, limit int) ([]string, error) {
	// GROUP BY with the aggregate aliased into the select list keeps the
	// ordering legal under both engines' rules for DISTINCT/GROUP BY, and
	// created_at sorts correctly as text because timeFormat is a fixed-width
	// UTC ISO-8601 timestamp.
	rows, err := s.db.QueryContext(ctx, `
		SELECT s.seed, MAX(s.created_at) AS last_scan
		FROM findings f
		JOIN scans s ON s.id = f.scan_id
		WHERE f.candidate = ? AND s.seed <> ?
		GROUP BY s.seed
		ORDER BY last_scan DESC, s.seed
		LIMIT ?`, candidate, excluding, limit)
	if err != nil {
		return nil, fmt.Errorf("store: finding seeds recording %s: %w", candidate, err)
	}
	defer func() { _ = rows.Close() }()

	var seeds []string
	for rows.Next() {
		var seed, lastScan string
		if err := rows.Scan(&seed, &lastScan); err != nil {
			return nil, fmt.Errorf("store: finding seeds recording %s: %w", candidate, err)
		}
		seeds = append(seeds, seed)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: finding seeds recording %s: %w", candidate, err)
	}
	return seeds, nil
}

// exists runs a query written to return at most one row and reports whether it
// returned one.
func (s *SQLite) exists(ctx context.Context, query string, args ...any) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// The two triage reads are spelled out as whole statements rather than composed
// from a shared prefix and an ORDER BY: assembling SQL by concatenation is the
// habit worth not having, even where every fragment is a local constant.
const (
	selectTriageByCandidate = `
		SELECT seed, candidate, status, note, updated_at
		FROM triage
		WHERE seed = ?
		ORDER BY candidate`

	selectTriageByUpdatedAt = `
		SELECT seed, candidate, status, note, updated_at
		FROM triage
		WHERE seed = ?
		ORDER BY updated_at DESC, candidate`
)

// GetTriage implements Store.
func (s *SQLite) GetTriage(ctx context.Context, seed string) (map[string]Triage, error) {
	seed = NormalizeSeed(seed)
	rows, err := s.queryTriage(ctx, selectTriageByCandidate, seed)
	if err != nil {
		return nil, err
	}
	byCandidate := make(map[string]Triage, len(rows))
	for _, t := range rows {
		byCandidate[t.Candidate] = t
	}
	return byCandidate, nil
}

// ListTriage implements Store.
//
// Most recently decided first: a triage listing is read as a worklog, and the
// decision just made is the one being checked.
func (s *SQLite) ListTriage(ctx context.Context, seed string) ([]Triage, error) {
	return s.queryTriage(ctx, selectTriageByUpdatedAt, NormalizeSeed(seed))
}

// SetTriage implements Store.
//
// The upsert stays inside the portable subset — a column-list ON CONFLICT
// target and lowercase excluded. — so the same statement runs unmodified on
// Postgres after the phase-2 cutover.
func (s *SQLite) SetTriage(ctx context.Context, seed, candidate string, status triage.Status, note string) (Triage, error) {
	seed = NormalizeSeed(seed)
	candidate = NormalizeCandidate(candidate)
	switch {
	case seed == "":
		return Triage{}, errors.New("store: empty seed")
	case candidate == "":
		return Triage{}, errors.New("store: empty candidate")
	case !status.Valid():
		// The database is the last place a bad verdict can be caught, and a
		// status column is only worth reading if everything in it is meaningful.
		return Triage{}, fmt.Errorf("store: invalid triage status %q", status)
	}

	var (
		stored    Triage
		updatedAt string
	)
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO triage (seed, candidate, status, note, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (seed, candidate) DO UPDATE SET
			status = excluded.status,
			note = excluded.note,
			updated_at = excluded.updated_at
		RETURNING seed, candidate, status, note, updated_at`,
		seed, candidate, string(status), note, time.Now().UTC().Format(timeFormat),
	).Scan(&stored.Seed, &stored.Candidate, &stored.Status, &stored.Note, &updatedAt)
	if err != nil {
		return Triage{}, fmt.Errorf("store: recording triage for %s: %w", candidate, err)
	}
	if stored.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return Triage{}, err
	}
	return stored, nil
}

// queryTriage runs one of this file's triage SELECTs and scans the result.
func (s *SQLite) queryTriage(ctx context.Context, query, seed string) ([]Triage, error) {
	rows, err := s.db.QueryContext(ctx, query, seed)
	if err != nil {
		return nil, fmt.Errorf("store: reading triage for %s: %w", seed, err)
	}
	defer func() { _ = rows.Close() }()

	var out []Triage
	for rows.Next() {
		var (
			t         Triage
			updatedAt string
		)
		if err := rows.Scan(&t.Seed, &t.Candidate, &t.Status, &t.Note, &updatedAt); err != nil {
			return nil, fmt.Errorf("store: reading triage for %s: %w", seed, err)
		}
		if t.UpdatedAt, err = parseTime(updatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: reading triage for %s: %w", seed, err)
	}
	return out, nil
}

// findings reads every finding of one scan, in insertion order so a stored scan
// reads back in the deterministic order the engine produced it.
func (s *SQLite) findings(ctx context.Context, scanID int64) ([]Finding, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT candidate, registrable, technique,
		       registered, has_ns, has_a, has_mx, wildcard,
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
			f                                        Finding
			registered, hasNS, hasA, hasMX, wildcard int
			addresses, nameservers, mailservers      string
		)
		if err := rows.Scan(
			&f.Candidate, &f.Registrable, &f.Technique,
			&registered, &hasNS, &hasA, &hasMX, &wildcard,
			&addresses, &nameservers, &mailservers, &f.Rcode, &f.Error,
		); err != nil {
			return nil, fmt.Errorf("store: reading findings of scan %d: %w", scanID, err)
		}
		f.Registered = registered != 0
		f.HasNS = hasNS != 0
		f.HasA = hasA != 0
		f.HasMX = hasMX != 0
		f.Wildcard = wildcard != 0
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

// joinList encodes a record slice as a JSON array, which round-trips any
// byte a DNS answer can carry. An empty slice stays an empty string so the
// column reads as "nothing" under `sqlite3 <db> .dump`.
func joinList(values []string) string {
	if len(values) == 0 {
		return ""
	}
	// Marshalling a []string cannot fail.
	encoded, _ := json.Marshal(values)
	return string(encoded)
}

// splitList decodes a stored record list: JSON for rows written today, the
// legacy comma join for rows written before the encoding changed. No DNS
// name or IP address starts with "[", so the two are distinguishable.
func splitList(value string) []string {
	if value == "" {
		return nil
	}
	if strings.HasPrefix(value, "[") {
		var out []string
		if err := json.Unmarshal([]byte(value), &out); err == nil {
			if len(out) == 0 {
				return nil
			}
			return out
		}
	}
	return strings.Split(value, listSeparator)
}

// parseTime reads a stored timestamp. Parsing uses RFC3339Nano rather than
// timeFormat because Go's parser is lenient about fraction width under that
// layout — it reads both the fixed-width form written today and any
// variable-width RFC 3339 value an older database may hold.
func parseTime(value string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("store: unparseable timestamp %q: %w", value, err)
	}
	return t.UTC(), nil
}
