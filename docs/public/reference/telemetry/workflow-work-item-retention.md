# Workflow Work Item Retention

`workflow_work_items` keeps one row per collector target per cycle. Without
retention it grows forever: a permanently failing collector on a 15-minute
cycle adds about 96 terminal rows per target per day, and every status read
that groups or filters by status costs more each day (#8829). The workflow
coordinator can delete old terminal rows in bounded batches.

## Turning it on

Retention is **off by default**. A pass deletes history that cannot be
restored, so an operator opts in per deployment by setting the age. It runs
only in active deployment mode.

| Variable | Default | Meaning |
| --- | --- | --- |
| `ESHU_WORKFLOW_COORDINATOR_WORK_ITEM_RETENTION_AGE` | unset (off) | How long a terminal row is kept after its last update. Setting it turns retention on; `720h` is the recommended value. Must be at least `24h`; `0` or unset is off. |
| `ESHU_WORKFLOW_COORDINATOR_WORK_ITEM_RETENTION_BATCH_SIZE` | `1000` | Rows one batch transaction examines, and the most it deletes. Between 1 and 10,000. |

The rest of the policy is fixed: a pass runs every 5 minutes (the first at
startup), commits at most 100 batch transactions and then resumes from its
cursor on the next pass, and each batch has a 2-second `lock_timeout` and a
30-second `statement_timeout`.

Invalid values fail coordinator startup even while retention is off. With
retention on and no retention store wired, `Service.Run` fails instead of
silently never pruning.

## What a pass deletes

A pass walks the table in primary-key order, one batch transaction per
window. A row is deleted only when all of these hold:

- its status is `completed` or `failed_terminal`;
- its `updated_at` is older than the configured age;
- its run is closed (`complete` or `failed`), so run reconciliation never
  sees an item disappear from an open run;
- it is not the row of its scope's active generation, which collector
  readiness evidence joins;
- a newer row exists for the same `(collector_kind, collector_instance_id,
  scope_id)` target, so latest-row reads keep their answer;
- for `failed_terminal`, a newer `failed_terminal` row exists for the same
  target, so failure reporting keeps the newest terminal failure.

`claimed`, `pending`, `failed_retryable`, and `expired` rows are never
deleted. Claim rows in `workflow_claims` go with their work item through the
existing `ON DELETE CASCADE`.

Cumulative counts change once retention runs: totals such as
`terminal_failed_count` in collector back-pressure status and the completed
count in the coordinator snapshot drop as old rows go. The 30-minute
recent-failure window and the 24-hour registry status window are untouched
at any allowed age.

## Concurrency

Each batch takes its rows with `FOR UPDATE ... SKIP LOCKED`, so it never
waits on an item row. Claim, heartbeat, completion, failure, release, and
reap statements lock only `claimed` or `pending` rows, which retention never
selects. The cascade locks only the finished claims of the deleted items,
which no other writer locks. Each batch runs under a local `lock_timeout`
and a 30-second `statement_timeout`. Several coordinators can run retention
at once: their batches skip each other's locked rows.

## Metrics

| Metric | Type | Labels | Meaning |
| --- | --- | --- | --- |
| `eshu_dp_workflow_coordinator_work_item_retention_passes_total` | counter | `outcome` | Retention passes, `success` or `error`. |
| `eshu_dp_workflow_coordinator_work_item_retention_pass_duration_seconds` | histogram | `outcome` | Wall time of one pass. |
| `eshu_dp_workflow_coordinator_work_item_retention_deleted_total` | counter | `status` | Rows deleted, by `completed` or `failed_terminal`. A failed pass still counts the batches it committed. |
| `eshu_dp_workflow_coordinator_work_item_retention_batch_duration_seconds` | histogram | none | One batch transaction's wall time, which is also its row-lock hold. |
| `eshu_dp_workflow_coordinator_work_items_table_bytes` | gauge | none | `pg_total_relation_size` of `workflow_work_items` (heap, indexes, TOAST) after the last pass. |
| `eshu_dp_workflow_coordinator_work_items_estimated_rows` | gauge | none | The planner's live-row estimate (`pg_class.reltuples`) after the last pass. |

Each batch statement also carries the shared Postgres store span and query
metrics under store name `workflow_work_item_retention`.

## Logs

Each pass writes one line. A successful pass logs at INFO with the message
`workflow work item retention pass`. A failed pass logs at ERROR with the
message `workflow work item retention pass failed` and an `error` field.
Both carry `outcome`, `deleted_completed`, `deleted_failed_terminal`,
`scanned_rows`, `batches`, `wrapped`, `longest_batch_ms`, `duration_ms`,
`retention_age`, `batch_size`, `max_batches`, and `table_bytes` with
`table_estimated_rows` (or `table_size_error`).

## Runbook

**`passes_total{outcome="error"}` is rising.** Read the ERROR log's `error`
field. A `lock timeout` (SQLSTATE 55P03) means something else holds a
finished claim row of a terminal item, which no shipped writer does: find
the holder with `pg_blocking_pids` before changing any setting. A
`statement timeout` means a batch plan regressed: capture
`EXPLAIN (ANALYZE, BUFFERS)` of the batch statement in
`go/internal/storage/postgres/retention/work/items/sql.go` against a copy of
the table. The coordinator keeps running and retries the same window on the
next tick.

**`work_items_table_bytes` keeps growing with retention on.** Deleted rows
free space only after autovacuum. Check `n_dead_tup` and `last_autovacuum`
for `workflow_work_items` in `pg_stat_user_tables`. If `deleted_total` is
flat, the remaining rows are protected: open runs that never closed, or one
newest row per target. Check run status counts for runs stuck open.

**`batch_duration_seconds` p99 above about 1 s.** Batches are measured near
50 ms per 1,000 rows. A slow batch holds its row locks longer; lower
`ESHU_WORKFLOW_COORDINATOR_WORK_ITEM_RETENTION_BATCH_SIZE` while the plan is
investigated, then restore it.

**To stop retention.** Unset `ESHU_WORKFLOW_COORDINATOR_WORK_ITEM_RETENTION_AGE`
and restart the coordinator. Deleted rows stay deleted; nothing else
changes.
