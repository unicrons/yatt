-- +goose Up
-- Portable subset only: INTEGER PRIMARY KEY, ISO-8601 TEXT timestamps and 1/0
-- booleans, so the eventual Postgres migration set is a mechanical translation
-- of this file rather than a redesign.

CREATE TABLE scans (
    id         INTEGER PRIMARY KEY,
    seed       TEXT NOT NULL,
    profile    TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);

CREATE INDEX idx_scans_seed_created_at ON scans (seed, created_at);

CREATE TABLE findings (
    id          INTEGER PRIMARY KEY,
    scan_id     INTEGER NOT NULL REFERENCES scans (id) ON DELETE CASCADE,
    candidate   TEXT    NOT NULL,
    registrable TEXT    NOT NULL DEFAULT '',
    technique   TEXT    NOT NULL DEFAULT '',
    registered  INTEGER NOT NULL DEFAULT 0,
    has_ns      INTEGER NOT NULL DEFAULT 0,
    has_a       INTEGER NOT NULL DEFAULT 0,
    has_mx      INTEGER NOT NULL DEFAULT 0,
    addresses   TEXT    NOT NULL DEFAULT '',
    ns          TEXT    NOT NULL DEFAULT '',
    mx          TEXT    NOT NULL DEFAULT '',
    rcode       TEXT    NOT NULL DEFAULT '',
    error       TEXT    NOT NULL DEFAULT ''
);

-- A candidate appears at most once per scan, so re-saving a scan cannot silently
-- double every row.
CREATE UNIQUE INDEX idx_findings_scan_candidate ON findings (scan_id, candidate);

-- +goose Down
DROP TABLE findings;

DROP TABLE scans;
