# Yet Another Typosquatting Tool (`yatt`)

`yatt` generates look-alike variants of a domain, resolves them over DNS, and reports which ones are
registered.

Unlike the stateless tools it takes its algorithms from, `yatt` remembers: scan history, cross-scan
diffs, and persistent triage state are the point. Re-scanning a seed tells you what *changed*, and a
verdict you record once is never asked of you again.

## Status

One permutation technique (omission), serial resolution, table/JSON/NDJSON output. Scans persist to
local SQLite with cross-scan diff (`new`/`changed`/`unchanged`/`gone`) and persistent triage state.
The remaining four techniques, concurrency, wildcard detection and scan profiles are still to come.

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
  -v, --verbose           log scan progress to stderr
```

```sh
yatt scan example.com
yatt scan example.com --output json | jq '.[] | select(.registered)'
yatt scan example.com --resolver 1.1.1.1 --timeout 5s
```

Machine-readable output goes to stdout and progress goes to stderr, so piping to `jq` stays clean
even with `--verbose`.

## The seed is the first row

Every report leads with the seed domain itself, marked with the technique `original`. It is resolved
exactly like a candidate — same NS-Rcode registration check, same A and MX presence — so a look-alike
that shares the real domain's nameservers or mail setup is obvious at a glance instead of needing a
second lookup.

It is recorded and diffed like a candidate too, so a change to the real domain's own NS or MX is
surfaced by the next scan. It is *not* counted as a candidate in `yatt history`, it is never hidden
by `--status`/`--exclude-status`, and it carries no triage status of its own unless you record one:
`new` means "nobody has judged this yet", which is a statement about a backlog the protected domain
does not belong in.

## State: history, diff, and triage

Every scan is recorded, so the second scan of a seed reports each candidate as `new`, `changed`,
`unchanged` or `gone` against the previous one. "Changed" is defined over the boolean signals —
registered, NS, MX, A — so a CDN rotating its addresses is not a change, but a candidate becoming
registered or gaining MX is.

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
internal/resolver/   miekg/dns wrapper; the registered/unregistered signal
internal/scan/       orchestration: permute -> resolve -> persist -> diff -> triage
internal/store/      database/sql repository over SQLite; goose migrations (embed.FS)
internal/triage/     triage status vocabulary and transition rules
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
