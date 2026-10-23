# 8829: workflow_work_items retention evidence

Refs #8829. Evidence for the bounded `workflow_work_items` retention batch in
`go/internal/storage/postgres/retention/work/items/sql.go` and the coordinator
loop that runs it. This is a new maintenance path; it changes no existing
claim, heartbeat, completion or reap statement.

## Environment

Local PostgreSQL 18.6 (aarch64), the same major as `docker-compose.neo4j.yml`
(`postgres:18-alpine`). Scratch database built from the shipped
`workflowControlSchemaSQL` plus migration `001_ingestion_scopes.sql`, dropped
after the run. The host was loaded (load average 13 to 25), so absolute times
are upper bounds, not a reference-profile claim.

Seed: 50 scopes x 6,000 cycles on a 15-minute cadence (about 62 days) =
300,000 work items and 299,990 claim rows. Scopes 0-9 fail terminally every
cycle (the #8803 measured shape); scopes 10-49 complete with about 5%
terminal failures; the newest three cycles of scopes 40-49 are `pending`,
`claimed` and `failed_retryable` in open runs. 40 scopes carry an active
generation; 60,000 unrelated `ingestion_scopes` rows make the anti-join
realistic. Table size 185 MB (items) and 125 MB (claims).

## Performance Evidence

Performance Evidence: `EXPLAIN (ANALYZE, BUFFERS)` of one 1,000-row batch,
inside `BEGIN ... ROLLBACK`.

| Shape | Time | Shared buffers | Plan |
| --- | --- | --- | --- |
| PK keyset, `LIMIT` on candidates (first draft) | 81 ms | 32.8k | per-row probes; rows scanned unbounded when candidates are sparse |
| PK scan window, no `OFFSET 0` fences | 457 ms | 97.7k, temp 4.0k pages | hash semi-join over a 300k-row seq scan |
| PK scan window, `OFFSET 0` fences (shipped) | 54 ms | 38.8k | index-only PK window, per-row index probes |
| Shipped, `PREPARE`d custom plan, `$3` bound | 48 ms | 38.8k | same as above |
| Shipped, `plan_cache_mode = force_generic_plan` | 77 ms | 69.2k | same shape; the window hit failing scopes that need both probes |

Shipped plan, abridged:

```text
CTE scan_window
  -> Limit -> Index Only Scan using workflow_work_items_pkey (rows=1000, 223 buffers)
CTE candidates
  -> LockRows
     -> Nested Loop -> Nested Loop Anti Join -> Nested Loop
        -> CTE Scan on scan_window
        -> Index Scan using workflow_work_items_pkey on item (loops=1000)
             Filter: status/updated_at AND EXISTS(SubPlan 2) AND (completed OR EXISTS(SubPlan 3))
             SubPlan 2 -> Index Scan using workflow_work_items_collector_scope_generation_updated_idx (10.8k buffers)
             SubPlan 3 -> Index Scan using workflow_work_items_collector_scope_generation_updated_idx (10.8k buffers)
        -> Index Scan using ingestion_scopes_active_generation_idx (loops=1000)
        -> Index Scan using workflow_runs_pkey (loops=1000)
CTE deleted -> Delete on workflow_work_items (rows=1000)
Trigger for constraint workflow_claims_work_item_id_fkey: time=3.563 calls=1000
Execution Time: 53.751 ms
```

Full walk, one transaction per 1,000-row window, driven by `psql` (each
batch timing includes `psql` start-up):

| Walk | Batches | Deleted completed | Deleted failed_terminal | Slowest batch | Walk time |
| --- | --- | --- | --- | --- | --- |
| First | 301 | 118,598 | 37,452 | 74 ms | 12.7 s |
| Second (idempotence) | 144 | 0 | 0 | 25 ms | 3.8 s |

After the first walk: no row older than the cutoff was still eligible, all 50
scopes kept a `failed_terminal` row, every active generation row was kept,
the 10 `claimed`, 10 `pending` and 10 `failed_retryable` rows were untouched,
and `workflow_claims` held exactly one claim per remaining non-pending item
(143,940 = 143,940).

Rejected: a new `(collector_kind, scope_id, updated_at DESC)` index. The
per-row probes on the existing
`workflow_work_items_collector_scope_generation_updated_idx` cost about 11
buffers each and the batch already meets its budget; #8803 tracks that index
for its own read.

## Concurrency Evidence

Lock reasoning: the batch locks candidates `FOR UPDATE OF item SKIP LOCKED`.
Every lifecycle writer (claim, heartbeat, complete, fail, release, reap)
locks only `claimed` or `pending` items; retention selects only `completed`
and `failed_terminal`, which no runtime writer rewrites. The cascade locks
only finished claims of deleted items, which nothing else locks. Lock order
is item then claim, the same as the lifecycle statements. Each batch runs
under local `lock_timeout` (2 s) and `statement_timeout` (30 s).

Two-session scratch demonstrations:

| Demo | Setup | Result |
| --- | --- | --- |
| Foreign lock on a deletable row | Session A holds `FOR UPDATE` on the first row of the window and a heartbeat-style update on a claimed row for 6 s | Retention batch committed in 85 ms, deleted 999 of 1,000, the locked row remained |
| Open retention batch | Retention batch deletes 1,000 rows and sleeps 6 s before commit | Heartbeat, claim, claim-row insert, completion and an enqueue into a closed run all finished in 26 ms total under `lock_timeout = 1s` |

Go live proofs (`ESHU_POSTGRES_DSN`, one database per test), all PASS on the
same PostgreSQL 18.6:

- `TestPruneWorkItemsLiveDoesNotContendWithClaimLifecycle`: four workers
  claim, heartbeat and complete or terminally fail 300 live items through
  `WorkflowControlStore` while retention loops. Across five local runs (two
  under `-race`): 25 to 35 retention batches committed during the work, the
  slowest lifecycle operation took 6.8 to 15.5 ms (budget 2 s), the slowest
  retention batch took 45 to 47 ms, and each run deleted 2,940 history
  rows and left no orphan claims.
- `TestPruneWorkItemsLiveSkipsLockedRowsWithoutWaiting`: a held row lock is
  skipped, not waited on, and the row goes on the next walk. Removing
  `SKIP LOCKED` fails it with SQLSTATE 55P03.
- `TestPruneWorkItemsLiveOpenBatchDoesNotBlockLifecycle`: lifecycle work
  completes while an uncommitted batch holds its locks.
- `TestPruneWorkItemsLiveKeepsRowsNeededForFailureReporting`: keeps the
  newest unresolved terminal failure, the newest row and the active
  generation. Dropping the newest-failure guard fails it.

## Observability Evidence

Observability Evidence: `eshu_dp_workflow_coordinator_work_item_retention_passes_total{outcome}`,
`eshu_dp_workflow_coordinator_work_item_retention_pass_duration_seconds{outcome}`,
`eshu_dp_workflow_coordinator_work_item_retention_deleted_total{status}`,
`eshu_dp_workflow_coordinator_work_item_retention_batch_duration_seconds`
(lock hold), `eshu_dp_workflow_coordinator_work_items_table_bytes` and
`eshu_dp_workflow_coordinator_work_items_estimated_rows`, one INFO or ERROR
log line per pass, and Postgres store spans under
`workflow_work_item_retention`. Proven by
`TestNewMetricsPublishesWorkItemRetentionSignals` and
`TestServiceRunActiveModeRunsWorkItemRetentionWhenEnabled`. Operator page:
`docs/public/reference/telemetry/workflow-work-item-retention.md`.

## Default

Off by default: retention runs only when
`ESHU_WORKFLOW_COORDINATOR_WORK_ITEM_RETENTION_AGE` is set. A pass deletes
history that cannot be restored, and enabling it on upgrade would change
cumulative status counts without an operator decision. Precedent is mixed:
reducer generation retention is on by default
(`ESHU_GENERATION_RETENTION_ENABLED`), while its newer superseded search
index sweep (#8231) ships behind a default-off flag. This loop follows the
newer, gated precedent; turning it on later is one setting, and deletes
cannot be undone. The documented policy
(720h age, 1,000-row batches, 100 batches per pass, 5-minute interval) is the
conservative setting to turn on. Only the age and batch size are environment
settings: the generated `env-registry.md` reached the 500-line Markdown cap,
and the other bounds are safe fixed defaults.
