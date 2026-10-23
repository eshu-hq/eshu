# 8829: workflow_work_items retention evidence

Refs #8829. Evidence for the bounded `workflow_work_items` retention batch in
`go/internal/storage/postgres/retention/work/items/sql.go` and the coordinator
loop that runs it. This is a new maintenance path; it changes no existing
claim, heartbeat, completion or reap statement.

## Environment

Local PostgreSQL 18.6 (aarch64), the same major as `docker-compose.neo4j.yml`
(`postgres:18-alpine`). Scratch database built from the shipped
`workflowControlSchemaSQL` plus migration `001_ingestion_scopes.sql`, dropped
after the run. Host load average 4.5 to 11 during the measurement, so
absolute times are upper bounds, not a reference-profile claim.

Seed: 50 scopes x 6,000 cycles on a 15-minute cadence (about 62 days) =
300,000 work items (`md5` work item ids, so each primary-key window mixes
scopes) and 299,990 claim rows. Scopes 0-9 fail terminally every cycle (the
#8803 measured shape); scopes 10-49 complete with about 5% terminal
failures; the newest three cycles of scopes 40-49 are `pending`, `claimed`
and `failed_retryable` in open runs. 40 scopes carry an active generation at
cycle 1,000, older than the cutoff, so the anti-join removes real rows;
60,000 unrelated `ingestion_scopes` rows make it realistic. Table size
239 MB (items) and 127 MB (claims).

## Performance Evidence

Performance Evidence: `EXPLAIN (ANALYZE, BUFFERS)` of one 1,000-row batch as
a `PREPARE`d statement, inside `BEGIN ... ROLLBACK`, at four window starts
(`''`, `'4'`, `'8'`, `'c'`), three runs each, warm cache.

The arbiter ruling required keeping each target's newest `completed` row,
because the status registry's `last_completed_at`
(`go/internal/storage/postgres/status_registry.go`, `latest_completed`)
reads the newest completed row per collector kind. Three shapes were
measured:

| Shape | Time per batch | Shared buffers | Plan |
| --- | --- | --- | --- |
| A: newer-row probe plus newer-`failed_terminal` probe (previous head) | 15.6 to 16.8 ms | 14.6k to 15.2k | two per-row index probes |
| B: A plus a newer-`completed` probe | 18.4 to 20.5 ms | 16.1k to 16.8k | three per-row index probes |
| C: one newer-row probe with `newer.status = item.status` (shipped) | 15.3 to 16.4 ms | 14.1k to 14.7k | one per-row index probe |
| C without the `OFFSET 0` fence | 125.9 to 129.0 ms | 19.1k to 20.0k, temp 4.5k pages | hash semi-join over a full sequential scan |

C and B delete the same rows: a newer row of the same status is also a newer
row, so C keeps the newest row, the newest `completed` row and the newest
`failed_terminal` row of each target with one probe. The earlier note's
54 ms figure was taken on a busier host with a different seed and is
superseded.

Worst case: a target whose newest `completed` row is older than the cutoff
and followed by thousands of failures. Its probe heap-checks every newer row
of the target, because `status` is not in the index. A scope with 10
completions followed by 5,987 terminal failures, measured on the window that
holds its newest completion: C 15.0 to 15.2 ms and 15.7k buffers, A 15.1 to
15.5 ms and 15.0k buffers. The extra cost is bounded by the target's row
count, once per walk, and here the lapsed rows sat on adjacent heap pages;
scattered rows cost up to one buffer per newer row.

Second seed, the review recipe: the same 50 x 6,000 shape, but with
`aws:inst-1:gen-sNN-cNNNNN` ids, so each 1,000-row window is one scope's
consecutive cycles (174 MB). Same method, cutoff `2026-09-23`, three runs
per window, warm cache:

| Window | Previous two-probe statement | Shipped same-status probe |
| --- | --- | --- |
| head (scope 00, always failing) | 61.0 to 68.7 ms, 36.7k buffers | 35.9 to 36.2 ms, 27.4k buffers |
| scope 05, cycle 3,000 | 19.5 to 25.1 ms, 15.2k buffers | 11.4 ms, 10.4k buffers |
| scope 20 head | 32.5 to 32.6 ms, 28.2k buffers | 31.7 to 31.8 ms, 27.8k buffers |
| scope 20, cycle 1,500 | 65.1 to 68.1 ms, 46.5k buffers | 62.6 to 62.8 ms, 45.2k buffers |
| scope 45, cycle 1,000 | 51.7 to 56.0 ms, 40.9k buffers | 49.5 to 49.7 ms, 39.6k buffers |
| adversarial: 1 completed row then 5,996 failures, heap order shuffled | 42.0 to 42.4 ms, 42.4k buffers, deletes the completed row | 28.0 to 28.1 ms, 36.7k buffers, keeps it |

The 61 ms the review reproduced for the previous statement is the first row
of this table. Full walk on this seed plus the adversarial scope (305,997
rows), one `psql` per batch: 306 batches, 118,670 completed and 40,449
failed_terminal deleted, slowest batch 110 ms, 17.7 s; a second walk took
147 batches, deleted 0 and took 4.1 s. The review's walk of the previous
statement on this seed took 21.1 s. After the walk the adversarial scope
kept its one completed row, and every scope's newest `completed` and newest
`failed_terminal` `updated_at` matched an untouched copy.

Shipped plan, abridged:

```text
CTE scan_window
  -> Limit -> Index Only Scan using workflow_work_items_pkey (rows=1000, 532 buffers)
CTE candidates
  -> LockRows
     -> Nested Loop -> Nested Loop Anti Join -> Nested Loop
        -> CTE Scan on scan_window
        -> Index Scan using workflow_work_items_pkey on item (loops=1000)
             Filter: status/updated_at AND EXISTS(SubPlan 2)
             SubPlan 2 -> Index Scan using workflow_work_items_collector_scope_generation_updated_idx (loops=520)
                  Index Cond: collector_kind, scope_id, ROW(updated_at, work_item_id) > ROW(...)
                  Filter: collector_instance_id, status = item.status (2.4k buffers)
        -> Index Scan using ingestion_scopes_active_generation_idx (loops=520)
        -> Index Scan using workflow_runs_pkey (loops=520)
CTE deleted -> Delete on workflow_work_items (rows=520)
Trigger for constraint workflow_claims_work_item_id_fkey: time=2.844 calls=520
Execution Time: 14.339 ms
```

Full walk with the shipped statement, one transaction per 1,000-row window
under the shipped local timeouts, driven by `psql` (each batch timing
includes `psql` start-up):

| Walk | Batches | Deleted completed | Deleted failed_terminal | Slowest batch | Walk time |
| --- | --- | --- | --- | --- | --- |
| First | 301 | 118,679 | 37,331 | 72 ms | 12.9 s |
| Second (idempotence) | 144 | 0 | 0 | 30 ms | 4.5 s |

After the first walk, compared with an untouched copy of the seed: the
newest `completed` and newest `failed_terminal` `updated_at` of every scope
were unchanged, so `last_completed_at` per collector kind was unchanged; no
row older than the cutoff was still eligible; all 40 active-generation rows
were kept; the 10 `claimed`, 10 `pending` and 10 `failed_retryable` rows were
untouched; and `workflow_claims` held exactly one claim per remaining
non-pending item (143,980 = 143,980).

Rejected: a new `(collector_kind, scope_id, updated_at DESC)` index. The
per-row probe on the existing
`workflow_work_items_collector_scope_generation_updated_idx` already uses
`(updated_at, work_item_id)` as an index condition and the batch meets its
budget; #8803 tracks that index for its own read.

## Concurrency Evidence

Lock reasoning: the batch locks candidates `FOR UPDATE OF item SKIP LOCKED`.
Every lifecycle writer (claim, heartbeat, complete, fail, release, reap)
locks only `claimed` or `pending` items; retention selects only `completed`
and `failed_terminal`, which no runtime writer rewrites. The cascade locks
only finished claims of deleted items, which nothing else locks. Lock order
is item then claim, the same as the lifecycle statements. Each batch runs
under local `lock_timeout` (2 s) and `statement_timeout` (30 s). The
same-status probe takes no lock: it reads `workflow_work_items` through the
snapshot, so the lock set is the same as before.

Two-session scratch demonstrations, re-run with the shipped statement on a
fresh copy of the seed:

| Demo | Setup | Result |
| --- | --- | --- |
| Foreign lock on a deletable row | Session A holds `FOR UPDATE` on the first eligible row of the window and a heartbeat-style update on a claimed row for 6 s | Retention batch committed in 39 ms while A held its locks, deleted 519 of the window's 520 eligible rows, and the locked row remained |
| Open retention batch | Retention batch deletes 531 rows and sleeps 6 s before commit | Heartbeat, claim, claim-row insert, item and claim completion, and an enqueue into a closed run all finished in 11.8 ms total under `lock_timeout = 1s` |

Go live proofs (`ESHU_POSTGRES_DSN`, one database per test), all PASS on the
same PostgreSQL 18.6:

- `TestPruneWorkItemsLiveDoesNotContendWithClaimLifecycle`: four workers
  claim, heartbeat and complete or terminally fail 300 live items through
  `WorkflowControlStore` while retention loops one batch per call. A CI
  run once failed it with "the proof did not overlap": the workers drained
  the live work in 2.28 s before the first five-batch pass returned. An
  overlap gate (`overlap_gate_test.go`) now makes the overlap hold by
  construction. Workers start after retention's first deleting batch,
  retention runs at most one batch per four processed items ahead, and a
  worker takes claim ticket n only after n/10 retention batches, so at least
  30 batches commit while the workers run. With a 5 s delay injected
  before retention starts, the old proof failed with the CI message and the
  gated one passed. Across 45 runs under `-race` (`-count=20`,
  `GOMAXPROCS=1 -count=5`, and four concurrent `-count=5` copies): 37 to
  75 batches committed during the work, the slowest lifecycle operation
  took 7.5 to 72.0 ms (budget 2 s), the slowest retention batch took 39.5
  to 67.6 ms, and each run deleted 2,940 history rows and left no orphan
  claims.
- `TestPruneWorkItemsLiveSkipsLockedRowsWithoutWaiting`: a held row lock is
  skipped, not waited on, and the row goes on the next walk. The holder
  keeps its lock for the whole pass, so a batch that waited would end on
  the lock timeout; the proof checks the error, not a wall-clock bound.
  Removing `SKIP LOCKED` fails it with SQLSTATE 55P03.
- `TestPruneWorkItemsLiveOpenBatchDoesNotBlockLifecycle`: lifecycle work
  completes while an uncommitted batch holds its locks.
- `TestPruneWorkItemsLiveKeepsRegistryLastCompletedAt`: the review
  counterexample through `StatusStore.ReadStatusSnapshot`. A
  `package_registry` target completed 40 days ago and failed terminally
  from 39 to 31 days ago and 1 day ago. On the previous statement
  `last_completed_at` went from 2001-11-22 to absent; on the shipped one it
  keeps its date and only the nine superseded failures go.
- `TestPruneWorkItemsLiveKeepsRowsNeededForFailureReporting`: keeps the
  newest unresolved terminal failure, the newest completed row of a scope
  that lapsed into failures, the newest row and the active generation.
  Mutation checks on the shipped statement: dropping only the completed
  guard fails it (`lapse-1` deleted); dropping only the failed guard fails
  it (`heal-2` deleted); dropping both fails it on both.

## Coordinator Loop Evidence

The review found that a pass ran inline in `Service.Run`'s select loop, so a
100-batch pass (about 1.5 to 7 s measured, at worst 100 x the 30 s statement
timeout) delayed claim reaping and run reconciliation by its whole duration.
Retention now runs on one goroutine (`startWorkItemRetention`): a pass at
start, after the initial run reconciliation, then one per interval. At most
one pass is in flight; `time.Ticker` drops ticks during a long pass. `Run`'s
deferred stop cancels an in-flight pass and waits for the goroutine, so no
pass outlives `Run`.

The database overlap is not new. The same reap and reconcile statements
already ran beside retention from other coordinator replicas and from
collectors, which the live concurrency proofs above cover. Retention reads
`workflow_runs` without a lock, and run status only moves from open to
closed, so a pass that sees a run still open skips its rows until the next
pass. In-process shared state is the sweeper cursor (mutex-guarded), the
OpenTelemetry instruments, and the logger, all safe for concurrent use.

Proof: `TestServiceRunRetentionPassDoesNotDelayRunReconciliation` blocks a
pass until shutdown while run reconciliation ticks every 5 ms. Before the
change it timed out, because the startup pass held the loop. After it, two
reconciliation ticks land during the blocked pass, shutdown cancels it, and
`Run` returns after the pass reports. 30 runs under `-race` passed.

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
`TestServiceRunActiveModeRunsWorkItemRetentionWhenEnabled`; the default-on
startup pass with no environment set is proven by
`TestServiceRunActiveModeRunsDefaultRetentionWithNoEnvironment`. A pass
stopped by shutdown counts `outcome="canceled"` and logs INFO
`workflow work item retention pass canceled`
(`TestRunWorkItemRetentionTreatsShutdownAsCanceled`,
`TestRecordWorkItemRetentionCountsCanceledPassesApart`). The pass error,
not the context alone, decides `canceled`: the context must be done and the
error must wrap `context.Canceled`. A batch that fails with a lock or
statement timeout while shutdown begins still counts `outcome="error"`,
logs ERROR and reads the table size like any failure
(`TestRunWorkItemRetentionKeepsServerTimeoutsAsErrorsDuringShutdown`). The
sweeper's pre-flight rejections stay `error` with a done context
(`TestRunWorkItemRetentionKeepsPreflightErrorsAsErrorsDuringShutdown`), and
so does a `context.Canceled` error under a live context
(`TestRunWorkItemRetentionTreatsUnrequestedCancellationAsError`). Live
Postgres pins the driver side: a lock timeout surfaces as a
`*pgconn.PgError` with SQLSTATE 55P03 and a statement timeout as 57014,
neither matching `context.Canceled`, while a cancel during a waiting batch
surfaces as `context.Canceled` and rolls the batch back
(`cancel_live_test.go`). One driver gap needed a fix: `database/sql`
`Tx.Commit` returns `sql.ErrTxDone`, not the context error, once the
context watcher has rolled the transaction back. A local probe (200
commits right after a cancel) got `context.Canceled` on all 100 immediate
commits and `sql.ErrTxDone` on all 100 that waited 5 ms, so the sweeper
adds the context error to a commit that fails that way
(`TestPruneWorkItemsReportsACanceledCommitAsCancellation`). A batch whose
COMMIT is interrupted (a shutdown cancel or a dropped connection) is not
counted even if the server committed it; `deleted_total` misses at most
that one batch per interruption. The deletes are idempotent and the cursor
stays before that window, so the next pass re-walks it and finds the rows
gone. The table size
gauges export no data point until a pass reads the catalog size, so dark
mode, retention off and the time before the first pass are absent, not 0
(`TestWorkItemTableSizeGaugesStayEmptyWithoutAKnownReading`, and through
`Service.Run` with the production metrics
`TestWorkItemTableSizeGaugesFollowTheRetentionMode`: dark mode and age 0
export no point, an active pass exports one; removing the known-reading
check fails the dark and age-0 cases). Operator page:
`docs/public/reference/telemetry/workflow-work-item-retention.md`.

## Default

On by default, per the arbiter ruling on #8829. In active deployment mode the
coordinator runs retention at a 720h age with no setting;
`ESHU_WORKFLOW_COORDINATOR_WORK_ITEM_RETENTION_AGE=0` turns it off, and any
other age must be at least 48h. The 48h floor keeps a full day of margin past
the 24-hour registry status window for clock skew or a lagging read.

The ruling required, in the same change, the newest-`completed` keep-guard
above (otherwise the default would move the public `last_completed_at`), the
48h floor, and an operator upgrade note. The note is on
`docs/public/reference/telemetry/workflow-work-item-retention.md`: the first
pass runs at startup after run reconciliation, each pass examines at most
100 x batch size rows, a 268,800-row backlog drains in three passes (within
about 15 minutes), and cumulative status counts drop at upgrade and then
count only retained rows.

The pass interval (5 minutes), batches per pass (100), lock timeout (2 s)
and batch size default (1,000, range 1 to 10,000) are unchanged. No new
environment variable: the generated `env-registry.md` sits at the 500-line
Markdown cap, and the age entry gained its `720h` default in place.

Which deployments run it with no setting: the base Compose file and the Helm
chart default the coordinator to dark mode. The Compose overlays that set
active mode (`docker-compose.tier2-tfstate.yaml`,
`docker-compose.tier2-tfstate-v25.yaml`, `docker-compose.remote-e2e.runtime.yaml`,
and the component-extension examples under `docs/public/run-locally/`) and
any Helm install with claim-driven collectors
(`deploy/helm/eshu/templates/validate.yaml` requires active mode for them)
run it.
