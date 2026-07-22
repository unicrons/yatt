# Development

```sh
devbox run build    # go build ./...
devbox run test     # gotestsum -- ./...
devbox run lint     # golangci-lint run
```

## Layout

```text
cmd/                 Cobra commands
pkg/engine/          permutation techniques and seed parsing (reusable outside the CLI)
pkg/engine/data/     keyboard, homoglyph and TLD tables (see PROVENANCE.md)
internal/resolver/   miekg/dns wrapper; the registered/unregistered signal
internal/scan/       orchestration: permute -> resolve -> persist -> diff -> triage -> report
internal/wildcard/   per-zone catch-all detection
internal/store/      database/sql repository over SQLite; goose migrations (embed.FS)
internal/store/remote/  S3 transport for the store: lock object, download, upload-on-close
internal/triage/     triage status vocabulary and transition rules
internal/config/     scan profiles, config-file loading, and the precedence chain
internal/enrich/     AbuseIPDB / Shodan link builders (no HTTP calls)
internal/render/     table / JSON / NDJSON renderers and the live progress line
```

The `store` package is an interface over `database/sql` and the SQL stays inside the portable subset
(column-list `ON CONFLICT`, `RETURNING`, `1`/`0` booleans, ISO-8601 TEXT timestamps), with migrations
in one directory per engine. Triage history is the data that has to survive an eventual Postgres
cutover, so nothing is allowed to depend on SQLite specifics.

## Demo gifs

The gifs in the README are recorded with [vhs](https://github.com/charmbracelet/vhs) from the tapes
in `docs/vhs/`. The tapes run the locally built binary, so build it explicitly first —
`devbox run build` compiles the packages but leaves no `./yatt` behind:

```sh
go build -o yatt .
vhs docs/vhs/scan.tape
vhs docs/vhs/triage.tape
```
