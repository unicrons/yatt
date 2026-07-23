# CLI reference

## Commands and flags

```text
yatt scan <domain>                  permute, resolve, record, and report
yatt history <domain>               list the recorded scans of a seed
yatt diff <domain>                  compare the last two scans, without resolving
yatt triage <domain> <candidate>    record a verdict on a candidate
yatt triage list <domain>           list the verdicts recorded for a seed
yatt state push [local-db]          upload a local database to the remote location
yatt state pull <local-path>        download the remote database to a local file
yatt state unlock                   clear the lock a crashed process left behind

Global flags:
  -o, --output string     output format: table|json|ndjson (default "table")
      --resolver string   upstream DNS resolver as host[:port] (default: the system resolver)
      --timeout duration  per-query DNS timeout (default 3s)
      --db string         scan database path, or s3://bucket/key to keep it in S3
                          (default: yatt/yatt.db under the user config dir)
      --concurrency int   how many candidates to resolve at once (default 20)
      --qps float         cap DNS queries per second across all workers (0 for unlimited)
      --config string     config file (YAML/JSON/TOML) defining scan profiles (default: none)
      --punycode          show IDN candidates in their punycode (xn--) form instead of Unicode
  -v, --verbose           log scan progress to stderr

scan flags:
      --show-unregistered       also report candidates nobody has registered
      --technique strings       techniques to run: omission, transposition, keyboard, addition,
                                hyphenation, vowel-swap, bitsquatting, dot-insertion, tld,
                                homoglyph (default: all)
      --tld-profile string      TLD list the tld technique swaps against: common|full (default "common")
      --tld-file string         custom TLD list, one per line; overrides --tld-profile
      --limit int               cap the total candidate count after per-technique capping (0 for unlimited)
      --profile string          scan profile: quick|full, or one defined in --config
      --wide                    add the enrichment-link column to the table
      --status strings          report only candidates with these triage statuses
      --exclude-status strings  report every candidate except those with these statuses

triage flags:
      --status string   verdict to record: benign|suspicious|malicious|watchlist|ignored|
                        false_positive|owned
      --note string     free-text note recorded alongside the verdict
      --force           record the verdict even if the candidate has never appeared in a
                        scan of this seed

state flags:
      --force           push: replace the remote database if one already exists
                        pull: overwrite the local file if it already exists
                        unlock: clear the lock without asking for confirmation
```

## Output streams

Machine-readable output goes to stdout and progress goes to stderr, so piping to `jq` stays clean
even with `--verbose`.

While a scan runs in a terminal, a live progress line on stderr tracks completions, the registered
count so far, an ETA and the resolution speed. It only appears when stderr is a terminal — piping or
redirecting stderr (CI, cron, `2>/dev/null`) suppresses it automatically, no flag needed.

## IDN candidates and punycode

The table renders IDN candidates in their Unicode form — a homoglyph like `аpple.com` is shown as
the deception it is, not as `xn--pple-43d.com`. Pass `--punycode` to see the wire form instead. JSON
and NDJSON always keep the stable punycode form in `candidate` (it is the store and triage key) and
add a `unicode` field on the rows where the two differ.
