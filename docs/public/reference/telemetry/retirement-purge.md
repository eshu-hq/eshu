# Retirement Trigger And Purge Signals

The operator-driven repository retirement trigger (#8602) resolves
repository selectors, then runs the tombstone mark, the phase-1 fence, and
the narrowed purge (#7766) in order. The narrowed purge deletes a fenced
repository's scope rows plus its scope- and repo-keyed leftover rows, then
completes the tombstone. These are the operator signals: the trigger funnel
from `go/internal/query/admin/retirement/trigger.go`, and the purge signals
from `go/internal/storage/postgres/repository_retirements_purge.go`.

| Signal | Type | Use |
| --- | --- | --- |
| `eshu_dp_repository_retirements_total{outcome}` | counter | Trigger outcomes, one event per marked repository: `accepted` when the tombstone mark commits, then exactly one terminal outcome per marked repository (`completed`, `blocked`, `refused`, `failed`). A repository the call never marked stays counter-silent. Distinct from `eshu_dp_repository_retirements_purged_total` (purge completions only) and from `eshu_dp_canonical_repository_retirements_total` (the #7324 path-conflict retirement, unrelated to this operator path). |
| `repository retirement refused at fence` | WARN log | A fence refusal stopped the request before any purge (`repo_ids`, the bounded fence reason). The marked tombstone stays open; a retry with the same idempotency key resumes it. |
| `repository retirement mark failed` | WARN log | A mid-loop mark error stopped the request before any fence (#8614): `outcome` (the marked repositories carry it as the counted terminal — `refused` on conflict, `failed` on error — while the failing repository carries that terminal counter-silent), the failing `repo_id`, and the still-open `marked_repo_ids`. The 409/500 body carries one per-repository entry per requested repository; a retry with the same idempotency key resumes the open rows. |
| `admin_retirement_action` | audit | One governance audit event per decision: accepted, refused, blocked, completed, failed, and denied inputs. Carries the bounded reason code and hashed scope, never raw reasons or keys. |
| `eshu_dp_repository_retirements_purged_total` | counter | Completed operator retirements, one per completed tombstone, unlabelled. Emitted after the purge commit, so a crash between commit and emission under-counts rather than double-counts. |
| `repository retirement purged` | INFO log | One line per purged repository (`repo_id`, `retirement_id`, `scopes_deleted`). |
| `repository retirement blocked: terminal generations not reached` | WARN log | The `terminal_not_reached` exit: a scope generation went live after the fence, so the purge wrote nothing and the fenced tombstone stays open. Carries `repo_id`, `retirement_id`, `scope_ids`, and `blocking_statuses` read back from the terminal gate (for example `active:1`). Only a new fence can move the blocking generation. |

## Trigger Counter Evidence (#8602)

No-Regression Evidence: baseline `origin/feat-7766-retirement` at `f94ea2be`
registers only `eshu_dp_repository_retirements_purged_total`; after this PR,
`go test ./internal/telemetry -count=1` (registration, including
`TestNewInstrumentsNoError` covering the new counter) and
`go test ./internal/query/admin/retirement -count=1` (trigger funnel,
including terminal-outcome counting) both pass. Input shape is the existing
trigger unit fixtures: no live backend, no Postgres/Neo4j version change, and
no Cypher, queue, lease, worker, or batching change. Terminal counts are
unchanged: the purge still emits one
`eshu_dp_repository_retirements_purged_total` per completed tombstone, and
the trigger emits one `accepted` plus one terminal outcome per marked
repository. Safe because the change is additive counter registration plus one
`Add` per already-computed outcome at the existing emission site
(`countOutcome` in `go/internal/query/admin/retirement/trigger.go`, a
nil-instruments no-op); no statement, fence, or purge ordering changed.

Observability Evidence: new counter `eshu_dp_repository_retirements_total`
with closed `outcome` label (`accepted`, `refused`, `blocked`, `completed`,
`failed`), emitted from `countOutcome`/`countRefused` in
`go/internal/query/admin/retirement/trigger.go`. Existing signals unchanged:
WARN `repository retirement refused at fence`, INFO
`repository retirement purged`, WARN
`repository retirement blocked: terminal generations not reached`, and the
`admin_retirement_action` audit event.
