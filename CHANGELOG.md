# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

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
