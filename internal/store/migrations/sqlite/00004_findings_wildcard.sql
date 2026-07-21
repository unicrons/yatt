-- +goose Up
-- The wildcard flag was computed on every scan but never stored, so any row
-- read back from the database — a diff, a gone row, history — silently lost
-- it and rendered catch-all noise as real registered look-alikes. Existing
-- rows default to 0: their scans predate the column, and "not known to be a
-- wildcard" is the honest reading of them.

ALTER TABLE findings ADD COLUMN wildcard INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE findings DROP COLUMN wildcard;
