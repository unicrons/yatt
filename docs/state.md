# State: history, diff, and triage

Every scan is recorded, so the second scan of a seed reports each candidate as `new`, `changed`,
`unchanged` or `gone` against the previous one. "Changed" is defined over the boolean signals —
registered, NS, MX, A, wildcard — so a CDN rotating its addresses is not a change, but a candidate
becoming registered, gaining MX, or leaving a catch-all zone for real infrastructure is.

## Triage verdicts

Triage state is the other half. A verdict is keyed by seed and candidate rather than by scan, so it
survives every future run:

```sh
yatt triage example.com xample.com --status owned --note "defensive registration"
yatt triage example.com evil-example.com --status malicious

yatt scan example.com --exclude-status owned      # hide what we registered ourselves
yatt scan example.com --status new                # only the untriaged backlog
yatt scan example.com --status malicious,suspicious
```

Any settable status can move to any other, including itself to revise only the note. Nothing moves
back to `new`.

| Status | Meaning | Settable via `--status`? |
|---|---|---|
| `new` | nobody has judged this candidate yet | no — implicit only, and nothing can be set back to it |
| `benign` | judged harmless | yes |
| `suspicious` | worth watching, not yet proven bad | yes |
| `malicious` | confirmed abusive look-alike | yes |
| `watchlist` | keep surfacing regardless of verdict | yes |
| `ignored` | stop surfacing | yes |
| `false_positive` | the engine should not have produced this candidate | yes |
| `owned` | your organization registered it itself, defensively or otherwise | yes |

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

## Remote state in S3

The database can live in S3 instead of on local disk, so several machines — a laptop and a cron box,
say — share one history and one set of verdicts. Point `--db` at an object URL and every command
works unchanged:

```sh
yatt state push --db s3://my-bucket/yatt/yatt.db     # one-time migration of the local database
yatt scan example.com --db s3://my-bucket/yatt/yatt.db
```

Credentials come from the standard AWS chain (environment, shared config, SSO, instance roles), and
S3-compatible endpoints (MinIO, R2, …) from `AWS_ENDPOINT_URL_S3`. The bucket's region is discovered
from the bucket itself, so it does not need to match your configured region — no `AWS_REGION`
required.

Each command acquires a lock object (`<key>.lock`) before touching the database — created atomically
with a conditional write, so two clients cannot both win — then downloads the database, works on the
local copy, uploads it back if it changed, and releases the lock. Reads lock too: a second command
starting while one is running aborts immediately, naming the holder:

```
error: remote state is locked: held by laptop.local (pid 4242) since 2026-07-21T14:03:11Z, yatt
devel, operation "scan" — if that process is gone, run: yatt state unlock --db s3://…
```

A process that dies mid-command leaves that lock behind; `yatt state unlock` shows the holder and
clears it after confirmation. Only clear a lock whose process is actually gone — behind a
force-cleared lock the final upload is still conditional on the object version the run started from,
so a concurrent writer surfaces as a loud upload error rather than a silent overwrite, but the run
that loses that race has to be redone.

`state pull` takes a consistent local snapshot (handy for backups); `state push --force` restores
one, or uploads the copy a failed upload preserved. Don't mix modes: once a database is pushed to
S3, retire the local file — a machine still scanning against its local copy forks the history the
remote one exists to share.
