# Workflow Work Item Retention

`workflow_work_items` keeps one row per collector target per cycle. Without
retention it grows forever: a permanently failing collector on a 15-minute
cycle adds about 96 terminal rows per target per day, and every status read
that groups or filters by status costs more each day (#8829). The workflow
coordinator deletes old terminal rows in bounded batches.

## Default and turning it off

Retention is **on by default** at a 720-hour (30-day) age. It runs only in
active deployment mode. Set the age to `0` to turn it off. Deleted history
cannot be restored.

| Variable | Default | Meaning |
| --- | --- | --- |
| `ESHU_WORKFLOW_COORDINATOR_WORK_ITEM_RETENTION_AGE` | `720h` | How long a terminal row is kept after its last update. `0` turns retention off; any other value must be at least `48h`. |
| `ESHU_WORKFLOW_COORDINATOR_WORK_ITEM_RETENTION_BATCH_SIZE` | `1000` | Rows one batch transaction examines, and the most it deletes. Between 1 and 10,000. |

The rest of the policy is fixed: a pass runs every 5 minutes (the first at
startup), commits at most 100 batch transactions and then resumes from its
cursor on the next pass, and each batch has a 2-second `lock_timeout` and a
30-second `statement_timeout`.

Invalid values fail coordinator startup even while retention is off. With
retention on and no retention store wired, `Service.Run` fails instead of
silently never pruning.

Which deployments run it: the base Compose file and the Helm chart default
the coordinator to dark mode, where retention does not run. The Compose
overlays that set `ESHU_WORKFLOW_COORDINATOR_DEPLOYMENT_MODE: active` (the
Terraform-state tier-2 and remote end-to-end runtimes, and the
component-extension examples) and any Helm install with claim-driven
collectors, which requires `workflowCoordinator.deploymentMode=active`, run
it with no extra setting.

### Upgrade note

The first pass runs at coordinator startup, right after run reconciliation.
Each pass examines at most 100 times the batch size rows (100,000 at the
default), so a large backlog drains over several passes. A 268,800-row
backlog takes three passes: at startup, then 5 and 10 minutes later, so it
is done within about 15 minutes. The batch work itself is short: a full walk
of a 300,000-row table took about 300 batches and 13 to 18 seconds in the
#8829 measurements.

Cumulative counts drop at upgrade, when the backlog goes, and from then on
count only retained rows (terminal rows newer than the age, plus the rows
retention keeps). This applies to:

- collector back-pressure status: `terminal_failed_count` and the
  `failure_classes` counts;
- the coordinator snapshot's work-item status counts;
- registry collector status: `terminal_failures` (all-time, no window) and
  the `failure_classes` counts;
- registry collector status `metadata_targets`: the `planned`, `completed`,
  `failed`, and `rate_limited` counts per package ecosystem.

Latest-value reads keep their answer: each target keeps its newest row, its
newest `completed` row, and its newest `failed_terminal` row, so the
registry `last_completed_at` and the newest failure per target do not
change.

Windowed reads are untouched at any allowed age: the 30-minute recent-failure
window and the 24-hour registry status window only count rows far younger
than 48 hours.

## What a pass deletes

A pass walks the table in primary-key order, one batch transaction per
window. A row is deleted only when all of these hold:

- its status is `completed` or `failed_terminal`;
- its `updated_at` is older than the configured age;
- its run is closed (`complete` or `failed`), so run reconciliation never
  sees an item disappear from an open run;
- it is not the row of its scope's active generation, which collector
  readiness evidence joins;
- a newer row with the same status exists for the same `(collector_kind,
  collector_instance_id, scope_id)` target. So each target keeps its newest
  `completed` row, which the registry status `last_completed_at` reads; its
  newest `failed_terminal` row, which failure reporting reads; and its newest
  row of any status, which latest-row reads use.

`claimed`, `pending`, `failed_retryable`, and `expired` rows are never
deleted. Claim rows in `workflow_claims` go with their work item through the
existing `ON DELETE CASCADE`.

## Concurrency

Each batch takes its rows with `FOR UPDATE ... SKIP LOCKED`, so it never
waits on an item row. Claim, heartbeat, completion, failure, release, and
reap statements lock only `claimed` or `pending` rows, which retention never
selects. The cascade locks only the finished claims of the deleted items,
which no other writer locks. Each batch runs under a local `lock_timeout`
and a 30-second `statement_timeout`. Several coordinators can run retention
at once: their batches skip each other's locked rows.

Inside the coordinator, retention runs on its own loop, apart from collector
reconciliation, claim reaping, and run reconciliation, so a slow pass never
delays them. At most one pass is in flight; ticks that arrive during a long
pass are dropped. Shutdown cancels an in-flight pass and waits for it to
stop; the batches it already committed stay committed.

## Metrics

| Metric | Type | Labels | Meaning |
| --- | --- | --- | --- |
| `eshu_dp_workflow_coordinator_work_item_retention_passes_total` | counter | `outcome` | Retention passes: `success`, `error`, or `canceled` (stopped by coordinator shutdown; a batch that fails on its own during shutdown still counts as `error`). |
| `eshu_dp_workflow_coordinator_work_item_retention_pass_duration_seconds` | histogram | `outcome` | Wall time of one pass. |
| `eshu_dp_workflow_coordinator_work_item_retention_deleted_total` | counter | `status` | Rows deleted, by `completed` or `failed_terminal`. A failed or canceled pass still counts the batches it committed. The one exception is a shutdown cancel that lands during a batch's COMMIT round trip: the server can commit while the pass sees a cancellation, so this counter misses at most that one batch. The deletes are idempotent and the cursor stays before that window. |
| `eshu_dp_workflow_coordinator_work_item_retention_batch_duration_seconds` | histogram | none | One batch transaction's wall time, which is also its row-lock hold. |
| `eshu_dp_workflow_coordinator_work_items_table_bytes` | gauge | none | `pg_total_relation_size` of `workflow_work_items` (heap, indexes, TOAST) after the last pass that read it. |
| `eshu_dp_workflow_coordinator_work_items_estimated_rows` | gauge | none | The planner's live-row estimate (`pg_class.reltuples`) after the last pass that read it. |

The two table gauges export no data point until a pass has read the catalog
size: in dark mode, with retention turned off, and before the first pass
they are absent, not 0.

Each batch statement also carries the shared Postgres store span and query
metrics under store name `workflow_work_item_retention`.

## Logs

Each pass writes one line. A successful pass logs at INFO with the message
`workflow work item retention pass`. A failed pass logs at ERROR with the
message `workflow work item retention pass failed` and an `error` field. A
pass stopped by coordinator shutdown logs at INFO with the message
`workflow work item retention pass canceled`. The pass error decides which:
a pass is canceled only when its error is a context cancellation and the
coordinator's context is done. A lock timeout (SQLSTATE 55P03) or statement
timeout (57014) that lands as shutdown begins is still a failed pass.
All three carry `outcome`, `deleted_completed`, `deleted_failed_terminal`,
`scanned_rows`, `batches`, `wrapped`, `longest_batch_ms`, `duration_ms`,
`retention_age`, `batch_size`, and `max_batches`. The success and failure lines also carry
`table_bytes` with `table_estimated_rows`, or `table_size_error` when the
catalog read failed. The canceled line carries no table size field: a
canceled pass skips the size read.

## Runbook

**`passes_total{outcome="error"}` is rising.** Read the ERROR log's `error`
field. A `lock timeout` (SQLSTATE 55P03) means a batch waited more than
2 seconds for a lock. Item rows are taken with `SKIP LOCKED`, so the wait is
on something else: a relation lock on `workflow_work_items` or
`workflow_claims` (a non-concurrent `CREATE INDEX`, an `ALTER TABLE` from a
schema migration or `EnsureSchema`, a manual `LOCK TABLE`), or a session
holding a finished claim row of a terminal item, which no shipped writer
does. Find the holder with `pg_blocking_pids` and `pg_locks` before
changing any setting. A
`statement timeout` means a batch plan regressed: capture
`EXPLAIN (ANALYZE, BUFFERS)` of the batch statement in
`go/internal/storage/postgres/retention/work/items/sql.go` against a copy of
the table. The coordinator keeps running and retries the same window on the
next tick.

**`work_items_table_bytes` keeps growing with retention on.** Deleted rows
free space only after autovacuum. Check `n_dead_tup` and `last_autovacuum`
for `workflow_work_items` in `pg_stat_user_tables`. If `deleted_total` is
flat, the remaining rows are protected. Retention keeps every row of a run
that is still open, every row younger than the age, and every `claimed`,
`pending`, `failed_retryable` or `expired` row, even in a closed run (a run
fails as soon as one item fails terminally). For each `(collector_kind,
collector_instance_id, scope_id)` target it keeps up to three more rows: its
newest row, its newest `completed` row, and its newest `failed_terminal`
row. It also keeps each scope's active-generation row. Check run status
counts for runs stuck open.

**`batch_duration_seconds` p99 above about 1 s.** Batches are measured at
11 to 63 ms per 1,000 rows on a 300,000-row table, depending on how the
windows fall across targets, and 110 ms at the slowest in a full walk. A
slow batch holds its row locks longer; lower
`ESHU_WORKFLOW_COORDINATOR_WORK_ITEM_RETENTION_BATCH_SIZE` while the plan is
investigated, then restore it.

**To stop retention.** Set `ESHU_WORKFLOW_COORDINATOR_WORK_ITEM_RETENTION_AGE=0`
and restart the coordinator. Unsetting it is not enough: unset means the
720-hour default. Deleted rows stay deleted; nothing else changes.
