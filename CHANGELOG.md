# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [0.3.0] - 2026-08-18

### Added

- `--abuseipdb-enrich`: opt-in, live AbuseIPDB Confidence of Abuse score for
  each resolved address, shown as an `ABUSE%` table column and
  `abuse_confidence_score` in JSON/NDJSON. Requires the `YATT_ABUSEIPDB_KEY`
  environment variable (never accepted as a CLI argument); scores are cached
  per address for one scan. `diff` is unaffected — it never re-resolves or
  makes live calls.
- `--abuseipdb-rate`: overrides the AbuseIPDB lookup rate (default 1 req/s,
  sized for the free tier) for accounts on a higher-throughput plan.
- `--verbose` now reports how many AbuseIPDB lookups are about to run (and at
  what rate) when `--abuseipdb-enrich` is set, since those lookups have no
  progress indicator of their own.

## [0.2.0] - 2026-07-23

### Changed

- Internal cleanup of the `engine` and `scan` packages — stdlib reuse
  (`slices`/`strings` helpers), simpler loops, and a hoisted immutable glyph
  table. CLI behavior is unchanged.
- `engine.Cap` no longer accepts a variadic `uncapped` technique list: the
  per-technique cap exemption is now derived from the `SuffixSwap` interface,
  so a suffix-swap technique is exempt automatically. **Breaking** for
  `pkg/engine` library users.

### Removed

- Exported `engine.TechniqueTLD` constant. **Breaking** for `pkg/engine`
  library users.

## [0.1.1] - 2026-07-22

### Added

- Remote state in S3 (`--db s3://bucket/key`) with conditional-write lock objects.
- `yatt state push|pull|unlock` subcommands.
- Automatic bucket-region discovery.

### Fixed

- Store close errors now fail the command.
- Data-loss/clobber windows in the remote flow: unclean close, unlock handover,
  atomic pull, no writes in read-only mode, case-insensitive URI scheme,
  endpoints that omit ETags.

## [0.1.0] - 2026-07-21

### Added

- Initial CLI: ten permutation techniques, concurrent DNS resolution with a QPS
  cap and per-zone wildcard/NXDOMAIN-suppression detection, SQLite scan history
  with cross-scan diff, persistent triage verdicts, scan profiles + config
  file, enrichment links, table/JSON/NDJSON output with live progress.
