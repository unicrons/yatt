# Yet Another Typosquatting Tool (`yatt`)

`yatt` generates look-alike variants of a domain, resolves them over DNS, and reports which ones are
registered.

Unlike the stateless tools it takes its algorithms from, `yatt` is built to remember: scan history,
cross-scan diffs, and persistent triage state are the point. Those land in the phases after this one —
what exists today is the end-to-end path.

## Status

Walking skeleton. One permutation technique (omission), serial resolution, table/JSON/NDJSON output.
No persistence yet.

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
yatt scan <domain> [flags]

Flags:
  -o, --output string     output format: table|json|ndjson (default "table")
      --resolver string   upstream DNS resolver as host[:port] (default: the system resolver)
      --timeout duration  per-query DNS timeout (default 3s)
  -v, --verbose           log scan progress to stderr
```

```sh
yatt scan example.com
yatt scan example.com --output json | jq '.[] | select(.registered)'
yatt scan example.com --resolver 1.1.1.1 --timeout 5s
```

Machine-readable output goes to stdout and progress goes to stderr, so piping to `jq` stays clean
even with `--verbose`.

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
internal/scan/       orchestration: permute -> resolve -> findings
internal/render/     table / JSON / NDJSON renderers
```

## Credits

The permutation algorithms are reimplemented in Go with reference to
[dnstwist](https://github.com/elceef/dnstwist) (Apache-2.0) and
[ail-typo-squatting](https://github.com/typosquatter/ail-typo-squatting) (BSD-2-Clause).
