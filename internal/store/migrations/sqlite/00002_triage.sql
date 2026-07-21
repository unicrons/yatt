-- +goose Up
-- Triage state is keyed by (seed, candidate) and deliberately carries no
-- foreign key to scans or findings. A verdict is a statement about a domain,
-- not about the run that happened to surface it, so it must survive the scan
-- being superseded, the candidate temporarily disappearing from the permutation
-- set, and eventually the whole SQLite database being migrated to Postgres.

CREATE TABLE triage (
    id         INTEGER PRIMARY KEY,
    seed       TEXT NOT NULL,
    candidate  TEXT NOT NULL,
    status     TEXT NOT NULL,
    note       TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL
);

-- The unique constraint is what makes the column-list ON CONFLICT upsert legal
-- on both SQLite and Postgres: re-triaging a candidate overwrites its verdict
-- rather than accumulating a second one.
CREATE UNIQUE INDEX idx_triage_seed_candidate ON triage (seed, candidate);

-- +goose Down
DROP TABLE triage;
