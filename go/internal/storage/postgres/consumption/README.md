# internal/storage/postgres/consumption

Paged scope rebuild for the package-consumption sidecars:
`package_manifest_consumption_keys` and `package_registry_identity_keys`.
The reducer's backfill in the parent package
(`package_manifest_consumption_backfill.go`) calls `Rebuilder.RebuildScope`
once for each scope it walks or repairs.

## Why it exists

Before #7663 one transaction rebuilt a whole scope. It held `FOR UPDATE` on
the scope's `ingestion_scopes` row from the first page to the last. On a
63,000-fact scope that transaction ran for more than an hour, and every insert
whose foreign key references the scope waited behind it or failed with
SQLSTATE 55P03. That included reducer writes to `fact_records` and
`shared_projection_acceptance`.

## Transaction shape

| Transaction | Holds | Does |
| --- | --- | --- |
| start | scope row `FOR NO KEY UPDATE`, `lock_timeout` | Deletes the scope's rows from both sidecars, then writes the dirty row (insert, or rewrite with `marked_at` kept) and returns its `xmin`. |
| page (one per 500 facts) | the same | Reads the next page by `fact_id`, deletes those facts' keys, inserts the derived keys, commits. |
| last page | the same | Deletes the dirty row only `WHERE xmin::text = <captured>`. |

- **Conflict domain**: the scope's `ingestion_scopes` row. `FOR NO KEY UPDATE`
  conflicts with the ingester's `upsertIngestionScopeQuery`, which is the only
  writer that sets the sidecar-writer GUC and so skips the dirty trigger. It
  does not conflict with `FOR KEY SHARE`, so foreign-key inserts never wait.
- **Readiness**: readers require the v2 marker and an empty dirty table in one
  repeatable-read snapshot. The fence row exists from the start commit to the
  last commit, so a partly rebuilt scope is never read as ready.
- **Why `xmin`**: the dirty trigger stamps `marked_at` with the writer's
  transaction start, which can be older than, or equal to, the rebuild's
  stamp. Every write to the row creates a new version, so a changed `xmin` is
  the reliable signal.
- **Retry scope**: the pass. A failed or canceled transaction rolls back only
  itself. The pass returns the error, the cursor does not advance, and the
  next pass rebuilds the scope from its start. Every step is idempotent:
  deletes by scope or fact id, then upserts.
- **Ordering**: scope row, then sidecar rows, then the dirty row. The
  ingester takes the scope row before its sidecar rows, so the two cannot
  deadlock on sidecar rows.

## Telemetry

- `eshu_dp_package_manifest_backfill_page_duration_seconds{outcome}`: one
  observation per transaction (`committed`, `lock_timeout`, `failed`).
- `eshu_dp_package_manifest_backfill_scope_lock_wait_seconds`: scope row lock
  wait per transaction.
- `eshu_dp_package_manifest_backfill_scope_rebuilds_total{phase,outcome}`:
  `cleared`, `left_dirty`, `skipped`, `disappeared`, `lock_timeout`, `failed`.

`Result` carries the same numbers for the parent's
`package_manifest_consumption_keys.backfill.progress` log line.

## Known cost

The two scope-wide sidecar deletes are sequential scans: neither table has a
scope-leading index yet (#7420). Their cost tracks the table, not the scope.
On the #7663 fixture each one took 61-85 ms at 1M rows and 252-449 ms at 5M
rows. They run once per rebuild, in the start transaction, which blocks only
the ingester for that scope.

## Progress limit

A transaction that hits `lock_timeout` fails the pass, the cursor does not
advance, and the next pass rebuilds that scope from its start. A scope that is
ingested almost continuously, or whose dirty row an old writer keeps rewriting,
can therefore time out again on every pass. Such a scope also keeps first place
in the initial-phase cursor and in the dirty repair's `marked_at` order, so the
scopes behind it wait. An operator sees `scope_failed outcome=lock_timeout
scope_id=...`, `scope_rebuilds_total{outcome="lock_timeout"}` rising, a flat
cursor, and readiness false. The old single transaction always made progress by
blocking writers; this design trades that for bounded writer waits. If a scope
livelocks, quiesce its ingestion or park the backfill with the leadership lock
(see the park-holder runbook). `DefaultLockTimeout` is a Go constant, so raising
it is a code change and a deploy, not an operator setting.
