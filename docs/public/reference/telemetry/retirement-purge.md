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
| `eshu_dp_repository_retirements_total{outcome}` | counter | Trigger outcomes, one event per repository: `accepted` when the tombstone mark commits, then exactly one terminal outcome per repository (`completed`, `blocked`, `refused`, `failed`). Distinct from `eshu_dp_repository_retirements_purged_total` (purge completions only) and from `eshu_dp_canonical_repository_retirements_total` (the #7324 path-conflict retirement, unrelated to this operator path). |
| `repository retirement refused at fence` | WARN log | A fence refusal stopped the request before any purge (`repo_ids`, the bounded fence reason). The marked tombstone stays open; a retry with the same idempotency key resumes it. |
| `admin_retirement_action` | audit | One governance audit event per decision: accepted, refused, blocked, completed, failed, and denied inputs. Carries the bounded reason code and hashed scope, never raw reasons or keys. |
| `eshu_dp_repository_retirements_purged_total` | counter | Completed operator retirements, one per completed tombstone, unlabelled. Emitted after the purge commit, so a crash between commit and emission under-counts rather than double-counts. |
| `repository retirement purged` | INFO log | One line per purged repository (`repo_id`, `retirement_id`, `scopes_deleted`). |
| `repository retirement blocked: terminal generations not reached` | WARN log | The `terminal_not_reached` exit: a scope generation went live after the fence, so the purge wrote nothing and the fenced tombstone stays open. Carries `repo_id`, `retirement_id`, `scope_ids`, and `blocking_statuses` read back from the terminal gate (for example `active:1`). Only a new fence can move the blocking generation. |
