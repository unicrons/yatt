-- +goose Up
-- The triage guard asks "has this candidate ever been recorded, under this seed
-- or any other?", which reads findings by candidate alone. The existing
-- idx_findings_scan_candidate cannot serve that lookup: its leading column is
-- the scan, so a search across scans degrades to a full table scan.

CREATE INDEX idx_findings_candidate ON findings (candidate);

-- +goose Down
DROP INDEX idx_findings_candidate;
