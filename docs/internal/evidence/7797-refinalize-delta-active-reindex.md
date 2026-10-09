# #7797 refinalize requests a full reindex for delta-active scopes

## Change

`RefinalizeScopeProjections` re-projects each scope through one generation.
When that generation is a delta (`scope_generations.is_delta`), it carries only
the files that changed since its baseline. So a rebuild onto a wiped graph
restores only those files. This change does three things:

- `rebuild/reset.AffectedGenerationsTemplate` also returns `is_delta`.
- Inside the same fenced refinalize transaction, the refinalize upserts one
  `repository_reindex_requests` row for each delta-active git default-branch
  scope. It uses the same statement as `POST /api/v0/admin/reindex`.
- The responses carry `delta_active_scopes` and `reindex_requests_written`.

A git ingester's scheduled selection forces a full snapshot when it next syncs
a requested repository, at most `ESHU_REPO_RECONCILE_MAX_PER_CYCLE` per cycle
and later while an in-flight or recently failed full holds the scope. A
webhook-only ingester forces it only when a webhook triggers that repository.

The upsert runs after `AcquireReducerClaimFence` and `EnqueueProjectorWork`
and before `ApplyPreRetirement`, not after the retirement. The lock set is the
same in either position, because the transaction already holds `EXCLUSIVE` on
`fact_work_items` and keeps every lock to commit; the reindex row locks are
only held through the two reset steps, and the contention test below proves no
lock cycle.

## Performance

Performance Evidence: the only read-path change is the `is_delta` column in
`AffectedGenerationsTemplate`. That statement runs once per operator
refinalize. It is not on an ingestion or query hot path.

Setup:

- Source: the baseline template is from `origin/main` `01ceb1dd0`. The changed
  template is from branch commit `c815fb26a`, which has the same patch-id as
  pre-rebase `72dda0cee`.
- Database: PostgreSQL 18.6, a private scratch cluster with the bootstrap
  schema.
- Data: 10,000 `ingestion_scopes` (8,000 active, 1,000 failed, 1,000 pending)
  and 30,000 `scope_generations` (three per scope). Every third active
  generation is a delta. Both tables were analyzed.
- Method: `EXPLAIN (ANALYZE, BUFFERS)`, eight runs per template, interleaved,
  with the first mover alternating. The host load average was about 20, so
  these figures are not a latency SLO.

| Template | Execution time per run (ms) |
| --- | --- |
| Baseline | 13.1, 14.5, 13.2, 16.8, 15.4, 15.9, 15.1, 15.3 |
| After | 39.8, 43.0, 43.0, 39.7, 49.4, 47.5, 43.3, 43.2 |

Result: about +28 ms per refinalize at 10,000 scopes.

Plan, after the change:

- It keeps the `ingestion_scopes_pkey` index scan, with no sort.
- It adds one `scope_generations_pkey` probe per active scope: 8,001 loops and
  24,003 shared-buffer hits.
- The failed-scope lateral is unchanged: 1,000 loops on
  `scope_generations_scope_latest_lookup_idx`.

Exactness: the first three output columns of the two templates are
byte-identical (`diff`: IDENTICAL). So the selection and the skip
classification do not change. Result classes: 6,002 full, 2,999 delta, and
1,000 `no_active_generation`.

Write path: one `INSERT ... ON CONFLICT DO UPDATE`, with one row per
delta-active git default-branch scope. The transaction already holds
`EXCLUSIVE` on `fact_work_items` from the claim fence, so the upsert makes that
window longer by one statement.

Not measured:

- The upsert duration at the production count, which is about 160 rows. I
  expect less than 1 ms.
- A Neo4j end-to-end run (T1).

Concurrency proof (PostgreSQL 18 live tests in
`recovery_refinalize_delta_active_fence_live_test.go`):

- Rollback: the test injects a failure after the upsert. No watermark and no
  projector item remain.
- Contention: the test holds the refinalize transaction after the upsert.
  - The collector watermark read does not block, and it does not see the
    uncommitted row.
  - The admin reindex upsert of the same scope waits on the row lock
    (`pg_stat_activity` `wait_event_type = 'Lock'`).
  - Both transactions then commit, with no deadlock.
- Lock order: both writers lock rows in sorted `scope_id` order.
  `TestRequestRepositoryReindexQueryMatchesRefinalize` pins the two statements
  byte-identical.
- Two refinalizes: they serialize at the `EXCLUSIVE` fence before either
  reaches the upsert.

## Observability

Observability Evidence: `RecoveryStore.reportDeltaActive` emits these signals
after commit:

- `eshu_dp_recovery_delta_active_scopes_total{outcome}`. The label set is
  closed: `reindex_requested` and `reindex_unsupported`. It never carries scope
  ids. The live test asserts `{reindex_requested: 2, reindex_unsupported: 2}`.
- The span attribute `eshu.recovery.delta_active_scopes` on the caller's
  request span.
- Per-scope logs with `scope_id`, `generation_id`, and `outcome`: the first
  `recovery.DeltaActiveScopeSampleLimit` (10) scopes at WARN, every further
  scope at INFO, then one summary WARN with `delta_active_total`, the exact
  `reindex_requested` and `reindex_unsupported` counts, `per_scope_warn_limit`,
  and `per_scope_info_count`. A recovery of about 160 delta-active scopes emits
  11 WARN lines, not 160 (`TestReportDeltaActiveCapsPerScopeWarnings`).

The handler's refinalize outcome log carries `delta_active_total` and moves to
WARN when that value is not zero. The response bodies carry
`delta_active_scopes` and `reindex_requests_written`.

Repair progress shows in
`eshu_dp_collector_reconciliation_full_snapshots_total{reason="repository_reindex_requested"}`
and in the `git_reconcile_forced` log for the requested `scope_id`.
