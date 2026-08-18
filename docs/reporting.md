# What a report contains

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

## Enrichment links

Every finding carries ready-to-open AbuseIPDB and Shodan lookup links: one pair by domain, plus one
pair per resolved address. Nothing here makes an HTTP call by default — these are links to open by
hand, not API calls this tool makes on your behalf, so there is no key to configure and no rate limit
to trip.

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

## AbuseIPDB confidence score (`--abuseipdb-enrich`)

`--abuseipdb-enrich` is the one opt-in exception to "no HTTP call ever leaves this tool": it looks up
each resolved address's AbuseIPDB **Confidence of Abuse** percentage and reports it alongside the
finding. Unlike the enrichment links above, this makes a real API call and spends AbuseIPDB API
credits, so it stays off unless you ask for it.

It needs a key in the `YATT_ABUSEIPDB_KEY` environment variable — never as a CLI argument, so it never
lands in shell history or `ps` output. Passing the flag without the variable set is a hard error,
before any DNS resolution happens:

```sh
export YATT_ABUSEIPDB_KEY=your-key-here
yatt scan example.com --abuseipdb-enrich
```

The table gains an `ABUSE%` column — independent of `--wide`, since a score is one short number, not
a full URL. `--output json`/`--output ndjson` set `abuse_confidence_score` on each address's AbuseIPDB
entry:

```sh
yatt scan example.com --abuseipdb-enrich --output json | jq '.[].enrichment[] | select(.name == "AbuseIPDB")'
```

Lookups are cached by address for the duration of one scan, so a run with many candidates resolving
to the same address only spends one API call on it — there is no cross-scan cache yet, so a later scan
of the same seed looks every address up again. A lookup that fails for one address (a timeout, a rate
limit) leaves that address without a score rather than failing the whole report; an invalid key fails
the run immediately, since every subsequent lookup would fail the same way.

Lookups run sequentially at a conservative default of one per second, and have no progress indicator
of their own — a scan turning up many unique addresses can sit quiet for that many seconds after the
DNS progress bar clears and before the report prints. `--verbose` prints how many lookups are about
to happen so that wait doesn't read as a hang:

```sh
yatt scan example.com --abuseipdb-enrich --verbose
# stderr: looking up 12 unique address(es) on AbuseIPDB (rate-limited to 1/s, ~12s)
```

The one-per-second default is sized for AbuseIPDB's free tier. If your plan allows a higher rate,
`--abuseipdb-rate` raises it — this also shrinks the silent wait above:

```sh
yatt scan example.com --abuseipdb-enrich --abuseipdb-rate 20
```

`diff` never re-resolves or makes live calls, so it has no `--abuseipdb-enrich`/`--abuseipdb-rate` flags.
