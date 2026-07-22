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
