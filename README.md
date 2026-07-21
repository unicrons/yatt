# Yet Another Typosquatting Tool (`yatt`)

`yatt` generates look-alike variants of a domain, resolves them over DNS, and reports which ones are
registered.

Unlike the stateless tools it takes its algorithms from, `yatt` remembers: scan history, cross-scan
diffs, and persistent triage state are the point. Re-scanning a seed tells you what *changed*, and a
verdict you record once is never asked of you again.

## Status

Five permutation techniques (omission, transposition, keyboard adjacency, TLD swap, homoglyph),
concurrent resolution under a QPS ceiling, per-zone wildcard detection, and table/JSON/NDJSON output.
Scans persist to local SQLite with cross-scan diff (`new`/`changed`/`unchanged`/`gone`) and persistent
triage state. Scan profiles, a config file, and enrichment links round out the first slice.

## Quickstart

```sh
devbox shell          # go, goose, golangci-lint, sqlite, gotestsum
go run . scan example.com
```

Or build a binary:

```sh
go build -o yatt .
./yatt scan example.com
```

## Usage

```text
yatt scan <domain>                  permute, resolve, record, and report
yatt history <domain>               list the recorded scans of a seed
yatt diff <domain>                  compare the last two scans, without resolving
yatt triage <domain> <candidate>    record a verdict on a candidate
yatt triage list <domain>           list the verdicts recorded for a seed

Global flags:
  -o, --output string     output format: table|json|ndjson (default "table")
      --resolver string   upstream DNS resolver as host[:port] (default: the system resolver)
      --timeout duration  per-query DNS timeout (default 3s)
      --db string         scan database path (default: yatt/yatt.db under the user config dir)
      --concurrency int   how many candidates to resolve at once (default 20)
      --qps float         cap DNS queries per second across all workers (0 for unlimited)
      --config string     config file (YAML/JSON/TOML) defining scan profiles (default: none)
  -v, --verbose           log scan progress to stderr

scan flags:
      --show-unregistered       also report candidates nobody has registered
      --technique strings       techniques to run: omission, transposition, keyboard, tld, homoglyph
                                (default: all)
      --tld-profile string      TLD list the tld technique swaps against: common|full (default "common")
      --tld-file string         custom TLD list, one per line; overrides --tld-profile
      --limit int               cap the total candidate count after per-technique capping (0 for unlimited)
      --profile string          scan profile: quick|full, or one defined in --config
      --wide                    add the enrichment-link column to the table
      --status strings          report only candidates with these triage statuses
      --exclude-status strings  report every candidate except those with these statuses
```

```sh
yatt scan example.com
yatt scan example.com --show-unregistered
yatt scan example.com --technique omission,homoglyph --limit 50
yatt scan example.com --output json | jq '.[] | select(.has_mx)'
yatt scan example.com --resolver 1.1.1.1 --timeout 5s
```

Machine-readable output goes to stdout and progress goes to stderr, so piping to `jq` stays clean
even with `--verbose`.

## Registered candidates only, registered candidates first

A scan reports only the candidates somebody has actually registered, and lists them ahead of
everything else. A `--tld-profile full` run generates several hundred names, nearly all of which have
never existed; the handful that someone took is the entire finding.

```sh
yatt scan example.com                       # only what is registered
yatt scan example.com --show-unregistered   # everything, registered first
```

Two kinds of row survive the default regardless:

- **The seed's own row**, on the same reasoning that exempts it from `--status`: it is the baseline
  the candidates are read against.
- **Any candidate whose lookup failed.** Nothing answered, which is not the same as the domain being
  free — dropping those would turn a partially-failed scan into a confidently short one. The failure
  is reported in the row's `error` field.

Ordering is a property of the report only. The sort is stable and the seed stays pinned to the first
row, so candidates keep the permutation engine's nearest-first order within each group and two scans
over identical answers still render identically — which is what the diff feature rests on. Nothing
here changes what was resolved or recorded: `--show-unregistered` re-reads the same stored scan.

Run with `--verbose` to see how many rows the default hid.

## The seed is the first row

Every report leads with the seed domain itself, marked with the technique `original`. It is resolved
exactly like a candidate — same NS-Rcode registration check, same A and MX presence — so a look-alike
that shares the real domain's nameservers or mail setup is obvious at a glance instead of needing a
second lookup.

It is recorded and diffed like a candidate too, so a change to the real domain's own NS or MX is
surfaced by the next scan. It is *not* counted as a candidate in `yatt history`, it is never hidden
by `--status`/`--exclude-status` or by the registered-only default even when the seed itself does not
resolve, and it carries no triage status of its own unless you record one:
`new` means "nobody has judged this yet", which is a statement about a backlog the protected domain
does not belong in.

## State: history, diff, and triage

Every scan is recorded, so the second scan of a seed reports each candidate as `new`, `changed`,
`unchanged` or `gone` against the previous one. "Changed" is defined over the boolean signals —
registered, NS, MX, A, wildcard — so a CDN rotating its addresses is not a change, but a candidate
becoming registered, gaining MX, or leaving a catch-all zone for real infrastructure is.

Triage state is the other half. A verdict is keyed by seed and candidate rather than by scan, so it
survives every future run:

```sh
yatt triage example.com xample.com --status owned --note "defensive registration"
yatt triage example.com evil-example.com --status malicious

yatt scan example.com --exclude-status owned      # hide what we registered ourselves
yatt scan example.com --status new                # only the untriaged backlog
yatt scan example.com --status malicious,suspicious
```

Valid statuses are `new`, `benign`, `suspicious`, `malicious`, `watchlist`, `ignored`,
`false_positive` and `owned`. `new` is the implicit state of a candidate nobody has judged — it is
never stored, and nothing can be set back to it.

Filtering applies to the report only. Every candidate is still resolved and recorded, so a filtered
scan does not leave gaps in the seed's history or make the next diff report phantom changes.

Recording a verdict is not a DNS signal, so it never makes a candidate report as `changed`.

A verdict is only readable through the seed it is filed under, so `yatt triage` refuses a candidate
that has never appeared in a recorded scan of that seed — otherwise the wrong seed stores happily and
the verdict is invisible forever. When another seed does have the candidate, the error names it:

```
$ yatt triage example.com nicrons.cloud --status owned
error: nicrons.cloud has never appeared in a scan of example.com, but it is recorded under
unicrons.cloud: did you mean `yatt triage unicrons.cloud nicrons.cloud`? (pass --force to record it
under example.com anyway)
```

The check reads recorded scans rather than re-deriving the permutation set, so a candidate an earlier
run produced still counts even if today's technique flags would not emit it. `--force` records the
verdict regardless — use it to pre-record a judgement before the seed's first scan, or to judge a
candidate the techniques in use did not produce. Such a verdict is stored, but stays invisible until
some scan surfaces the candidate.

## Scan profiles and the config file

`--profile` is a preset for the knobs that shape a scan: `techniques`, `tld_profile`, `concurrency`,
`qps`, `timeout` and `limit`. Two are built in:

- `quick` — omission, transposition and keyboard adjacency against the common TLD list, at modest
  concurrency, for a fast first look.
- `full` — every technique, including the two fan-out-heavy ones (homoglyph, TLD swap against the
  whole IANA list), at higher concurrency.

```sh
yatt scan example.com --profile quick
yatt scan example.com --profile full
```

A profile is a preset, not a lock: any flag it sets can still be overridden individually, and the flag
always wins.

```sh
yatt scan example.com --profile quick --technique homoglyph   # quick's techniques, but this one instead
yatt scan example.com --profile quick --tld-profile full      # quick's techniques, full's TLD list
```

`--config` points at a YAML/JSON/TOML file that can redefine a builtin profile or add new ones under
`profiles`, naming only the fields it wants to change — anything it leaves out still comes from the
builtin of the same name, or stays unset for a name with no builtin:

```yaml
# yatt.yaml
profile: quick        # selected when --profile is not given

profiles:
  quick:
    tld_profile: full  # quick, but sweeping the full TLD list instead of the curated one

  thorough:             # a profile with no builtin counterpart
    techniques: [omission, transposition, keyboard, tld]
    tld_profile: full
    concurrency: 30
    timeout: 5s          # a duration string; a bare number is rejected as ambiguous
    limit: 300
```

```sh
yatt --config yatt.yaml scan example.com --profile thorough
```

Precedence, low to high: the builtin profile < a same-named profile in the config file < the
`YATT_PROFILE` environment variable < an explicit `--profile` < a flag the command line actually set
for that one knob. A profile field left unset at every level does not blank a flag's own default —
scanning with no `--profile` at all behaves exactly as if profiles did not exist.

## Enrichment links

Every finding carries ready-to-open AbuseIPDB and Shodan lookup links: one pair by domain, plus one
pair per resolved address. Nothing here makes an HTTP call — these are links to open by hand, not API
calls this tool makes on your behalf, so there is no key to configure and no rate limit to trip.

```sh
yatt scan example.com --output json | jq '.[0].enrichment'
```

`--output json`/`--output ndjson` always include the full set, including one pair per resolved
address. The table leaves the links out by default — two full URLs per row, each derivable from the
candidate name, would wrap every other column off the terminal — and `--wide` adds an `ENRICH` column
carrying the by-domain pair:

```sh
yatt scan example.com --wide
```

## How "registered" is decided

Registration is read from the **response code of an NS query at the candidate's registrable domain
(eTLD+1)** — NXDOMAIN means unregistered, NOERROR means registered.

The naive alternative, treating an A-record NXDOMAIN as "unregistered", produces false negatives on
MX-only and delegation-only domains. Those are precisely the parked and defensive registrations a
typosquatting scan exists to surface, so A, AAAA, MX and NS presence are reported as separate signals
rather than as evidence of registration.

The seed is split with a public-suffix list rather than on the last dot, for the same reason: a naive
split of `example.co.uk` would query `co.uk`, which always answers NOERROR and would mark every
candidate registered.

## Development

```sh
devbox run build    # go build ./...
devbox run test     # gotestsum -- ./...
devbox run lint     # golangci-lint run
```

## Layout

```text
cmd/                 Cobra commands
pkg/engine/          permutation techniques and seed parsing (reusable outside the CLI)
pkg/engine/data/     generated keyboard, homoglyph and TLD tables (see PROVENANCE.md)
internal/resolver/   miekg/dns wrapper; the registered/unregistered signal
internal/scan/       orchestration: permute -> resolve -> persist -> diff -> triage -> report
internal/wildcard/   per-zone catch-all detection
internal/store/      database/sql repository over SQLite; goose migrations (embed.FS)
internal/triage/     triage status vocabulary and transition rules
internal/config/     scan profiles, config-file loading, and the precedence chain
internal/enrich/     AbuseIPDB / Shodan link builders (no HTTP calls)
internal/render/     table / JSON / NDJSON renderers
```

The `store` package is an interface over `database/sql` and the SQL stays inside the portable subset
(column-list `ON CONFLICT`, `RETURNING`, `1`/`0` booleans, ISO-8601 TEXT timestamps), with migrations
in one directory per engine. Triage history is the data that has to survive an eventual Postgres
cutover, so nothing is allowed to depend on SQLite specifics.

## Credits

The permutation algorithms are reimplemented in Go with reference to
[dnstwist](https://github.com/elceef/dnstwist) (Apache-2.0) and
[ail-typo-squatting](https://github.com/typosquatter/ail-typo-squatting) (BSD-2-Clause).

The homoglyph tables are derived from the Unicode Consortium's
[`confusables.txt`](https://www.unicode.org/Public/security/latest/confusables.txt), and the full TLD
list from the [IANA Root Zone Database](https://data.iana.org/TLD/tlds-alpha-by-domain.txt).
`pkg/engine/data/PROVENANCE.md` records the source behind every generated table.
