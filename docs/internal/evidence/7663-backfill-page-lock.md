# Package-consumption backfill: one transaction per page (#7663)

The reducer's package-consumption sidecar backfill rebuilt each scope in one
transaction. It held `FOR UPDATE` on the scope's `ingestion_scopes` row from
the first fact page to the last. In production, a 63k-fact scope held that
transaction for more than an hour. Up to four reducer writers inserting into
`fact_records` and `shared_projection_acceptance` for the scope waited behind
it or failed with SQLSTATE 55P03. This note records the cause, the change, and
the measurements behind both.

## Root cause

Root-Cause Evidence: two independent defects.

1. **The page query had no matching index.** The page SQL is
   `listPackageManifestConsumptionKeyScopeFactsSQL`:
   `scope_id = $1 AND fact_id > $2 AND <fact predicate> ORDER BY fact_id LIMIT 500`.
   `fact_id` is the fourth column of
   `fact_records_scope_generation_keyset_idx (scope_id, generation_id, observed_at, fact_id)`.
   On the fixture (2,608,426 facts, 409 scopes; scope `git:s0000` has 397,898
   facts over 6 generations and 31,836 manifest facts), a generic plan
   heap-fetched every fact of the scope above the cursor and sorted them.
   Page 1 cost 403k buffers, falling to 10.7k as the cursor advanced, and the
   64-page read loop took 10.9 s. A custom plan estimated rows=9 for 31,836
   and seq-scanned the table on early pages: 24.1 s for the loop.
2. **The scope row lock spanned the whole scope.** A foreign-key check on
   any insert that references `ingestion_scopes` takes `FOR KEY SHARE` on the
   scope row, and that conflicts with `FOR UPDATE`. Every such writer waited
   for the whole rebuild transaction, or hit its own `lock_timeout`.

## Change

- **Migration 181** adds `fact_records_package_manifest_consumption_backfill_idx`
  with `CREATE INDEX CONCURRENTLY` on `fact_records (scope_id, fact_id)`. Its
  `WHERE` clause is the page query's predicate, byte for byte.
  `TestPackageManifestConsumptionBackfillIndexPredicateMatchesPageSQL` derives
  that predicate from the Go constant, with a hermetic reassembly guard.
- **`consumption.Rebuilder`**, a new subpackage, replaces the single
  transaction:
  - The start transaction locks the scope row, deletes the scope's rows from
    both sidecars, and writes the dirty row (keeping `marked_at`). It captures
    the row's `xmin`.
  - One transaction per 500-fact page then locks the scope row, deletes the
    page's keys by `fact_id`, inserts the derived keys, and commits.
  - The last page deletes the dirty row `WHERE xmin::text = <captured>`.
  - Every transaction sets `lock_timeout = 5000ms`.
- The scope row lock is now `FOR NO KEY UPDATE` instead of `FOR UPDATE`.

## Concurrency

- **Conflict domain**: the scope's `ingestion_scopes` row.
  - `FOR NO KEY UPDATE` conflicts with the ingester's `upsertIngestionScopeQuery`.
    That writer is the only one that sets the sidecar-writer GUC
    (`ingestion.go`, before `upsertIngestionScope`); the backfill is the other
    GUC setter. Every other writer of a manifest or registry fact fires the
    dirty trigger.
  - `FOR NO KEY UPDATE` does not conflict with `FOR KEY SHARE`, so
    foreign-key inserts for the scope never wait on the backfill.
- **Transaction scope vs. retry scope**:
  - A transaction covers the start step or one page.
  - The retry unit is the pass. A failed or canceled transaction rolls back
    only itself, and the pass returns the error.
  - The initial-pass cursor advances only after a scope finishes, so the next
    pass rebuilds the scope from its start.
  - Every step is idempotent: deletes by scope or fact id, then upserts.
- **Readiness while partial**: readers
  (`query/supply/chain/impact/readiness_postgres.go`) require the v2 marker
  and an empty dirty table in one repeatable-read snapshot. The fence row
  exists from the start commit to the last commit.
- **Why `xmin`, not `marked_at`**: the dirty trigger (migration 139) sets
  `marked_at = now()`, which is the writer's transaction start. A writer that
  began before the rebuild stamps an older value. Every write to the row
  makes a new version, so `xmin` changes. A concurrent rebuild of the same
  scope also rewrites the row, so whichever rebuild finishes first leaves it
  dirty.
- **Ordering**: scope row, then sidecar rows, then the dirty row. The ingester
  takes the scope row before its sidecar rows, so the two cannot deadlock on
  sidecar rows.

## Measurements

Performance Evidence: PostgreSQL 18.6 (aarch64), the shared scratch host
(load average 34-57 during these runs), fixture database `p7663` above.

Page query (`EXPLAIN (ANALYZE, BUFFERS)`, prepared, 500 rows):

| plan | cursor | before (investigator) | after (migration 181) |
| --- | --- | --- | --- |
| generic | page 1 | 403k buffers, heap walk + sort | Index Scan on the new index, 507 buffers, 2.38 ms |
| generic | row 31,000 | 10.7k buffers | Index Scan, 507 buffers, 2.30 ms |
| custom | page 1 | Seq Scan, rows=9 estimate | Index Scan, 507 buffers, 2.15 ms |
| custom | row 31,000 | — | Index Scan, 507 buffers, 2.28 ms |

The 64-page read loop took 10.9 s (generic) and 24.1 s (custom) before. After,
it took 12 ms (generic) and 65 ms (custom), with a longest page of 6.4 ms and
7.7 ms. Index build: 1.16 s; size 14 MB.

Scope-list query (`listPackageManifestConsumptionKeyScopesSQL`, LIMIT 25):

| plan | cursor | before | after |
| --- | --- | --- | --- |
| custom | `''` | Parallel Index Scan + filter, 410.8 ms | Index Only Scan, 11,737 buffers, 5.4 ms |
| custom | `git:s0200` | Parallel Seq Scan + Sort, 392.3 ms | Index Only Scan, 224 buffers, 1.0 ms |
| generic | `''` | 285.9 ms | 5.4 ms |
| generic | `git:s0200` | 44.9 ms | 1.1 ms |

Whole-scope rebuild of `git:s0000`. A probe inserted a reducer-style
`fact_records` row for the same scope every 25 ms. The harness is the same
for both sides: before is origin/main `eef372440`'s
`rebuildPackageManifestConsumptionKeyScope`, after is this branch.

| run | rebuild wall | longest scope-row hold | probe worst insert latency |
| --- | --- | --- | --- |
| before, no index | 38,341 ms (one transaction) | 38,341 ms | 38,218 ms |
| before, with index | 1,332 ms (one transaction) | 1,332 ms | 1,302 ms |
| after, run 1 (cold) | 6,696 ms, 65 transactions | 384 ms | 86 ms |
| after, run 2 | 1,396 ms, 65 transactions | 39 ms | 14.4 ms |

The index fixes the per-page cost. The page transactions fix the lock: even
with the index, the old shape blocked the probe for the whole rebuild. Run 1's
wall time is a cold start on the loaded host; both after-runs committed 64
pages and 31,836 facts with outcome `cleared`.

Scope-wide sidecar deletes (start transaction): theory-first measurement.
Neither table has a scope-leading index (#7420 owns adding one), so each
delete is a sequential scan. Measured with `BEGIN`/`EXPLAIN ANALYZE`/`ROLLBACK`
on copies of each table:

| rows | table | 63,000-row scope | 10-row scope |
| --- | --- | --- | --- |
| 1M | package_manifest_consumption_keys | 79.4 ms | 61.7 ms |
| 1M | package_registry_identity_keys | 85.1 ms | 61.5 ms |
| 5M | package_manifest_consumption_keys | 449.2 ms | 305.7 ms |
| 5M | package_registry_identity_keys | 276.0 ms | 263.5 ms |

The cost tracks table size, not scope size. It runs once per rebuild, in the
start transaction, and under `FOR NO KEY UPDATE` it blocks only the ingester
for that scope. It is the remaining long pole of the start transaction on a
large sidecar, and #7420's index removes it. This PR adds no sidecar index.

## Proofs

Live proofs run on disposable PostgreSQL databases, gated by
`ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN`. Every one failed on origin/main
for the stated reason before the change:

- `TestPackageManifestConsumptionScopeRebuildReleasesScopeBetweenPagesLive`:
  stalls page 12 of 24. Before: 0 committed keys (want 5,500), and the
  reducer-style insert got 55P03. After: 5,500 keys committed, the insert
  completes, and the ingester's real scope upsert still gets 55P03.
- `TestPackageManifestConsumptionScopeRebuildLockTimeoutLeavesScopeUnreadyLive`:
  before, it never returned within 15 s. After, it gets 55P03 at 5,000 ms
  lock wait, with no cursor, no marker, and no keys, then converges.
- `TestPackageManifestConsumptionScopeRebuildResumesAfterPageFailureAndCancelLive`:
  before, it never returned within 20 s. After, page 6 fails with 55P03 and
  2,500 keys stay; a cancel in page 8 leaves 3,500; the rerun equals the
  independent derivation for both sidecars.
- `TestPackageManifestConsumptionScopeRebuildKeepsOldWriterDirtyLive`: before,
  the old writer did not commit within 2 s. After, it commits mid-repair, the
  scope stays dirty, and a later rebuild derives its key.
- `TestPackageManifestConsumptionScopeRebuildKeepsConcurrentWriterKeysLive`:
  holds on both. Writer keys behind and ahead of the cursor survive.
- `TestPackageManifestConsumptionScopeFactsPageUsesBackfillIndexLive`: before,
  it found a Sort node. After, it is an Index Scan on the new index, with no
  Sort, Seq Scan, or Filter, and 30 then 31 buffers for page 1 and page 8
  under both plan modes.
- `TestPackageManifestConsumptionBackfillPagesHeavyScopeLive` now also checks
  both sidecars against the independent derivation, and it passes on
  origin/main, so the old single-transaction result equals that derivation.

Mutation checks (each restored):

| mutation | test that fails |
| --- | --- |
| unconditional dirty delete | the old-writer test: 3,001 keys, 3,002 derived |
| `FOR UPDATE` | the between-pages test: reducer insert gets 55P03 |
| `FOR KEY SHARE` | the between-pages test: ingester upsert not excluded |

## Observability

Observability Evidence: each rebuild transaction records
`eshu_dp_package_manifest_backfill_page_duration_seconds{outcome}` and
`eshu_dp_package_manifest_backfill_scope_lock_wait_seconds`. Each scope
records
`eshu_dp_package_manifest_backfill_scope_rebuilds_total{phase,outcome}`.
`outcome` covers `lock_timeout` aborts and the dirty compare: `cleared` or
`left_dirty`.

The per-scope `package_manifest_consumption_keys.backfill.progress` line, and
the new `.scope_failed` line, carry `outcome`, `pages`, `facts`,
`transactions`, `max_transaction_ms`, `lock_wait_ms`, `max_lock_wait_ms`, and
`duration_ms`. A live run logged:
`outcome=lock_timeout pages=5 facts=2500 transactions=7 max_transaction_ms=5005`.

At 3 AM:

- A rising `lock_timeout` rate with `scope_lock_wait_seconds` near 5 s means
  ingester commits are holding scopes.
- Repeated `left_dirty` on one scope means an old writer is still writing it.

## Rejected hypotheses

- **`FOR NO KEY UPDATE` alone** lets foreign-key inserts through, but the
  rebuild stays one transaction of unbounded length that still blocks the
  ingester. That is not enough.
- **Comparing `marked_at`** misses a writer whose transaction started before
  the rebuild. Its `now()` is older.
- **A paged sidecar delete by `fact_id` range** costs roughly table size for a
  small scope, because the range spans other scopes' keys. Rejected in favor
  of the measured one-shot delete until #7420's index lands.

## Limits found in review

- Progress is not guaranteed for a scope that is ingested almost continuously:
  each timed-out pass restarts that scope. See the `Progress limit` section in
  `go/internal/storage/postgres/consumption/README.md`.
- A 55P03 can come from the dirty-scope row or a sidecar key row, not only the
  scope row. The `scope_failed` error text names which.
- The page size and the page query LIMIT are one constant, pinned by
  `TestPackageManifestConsumptionPageLimitEqualsRebuilderPageSize`.

## Follow-up: Result.Pages counts committed fact pages (review of #8248)

No-Regression Evidence: this follow-up changes one counter and adds tests; it
does not touch a lock, a transaction, a statement, a batch size, or a worker.
Baseline and after are the same code path on the same inputs: PostgreSQL 18.6,
the 7-fact, page-size-3 unit fixture (3 + 3 + 1 facts), and the 31,836-fact
scope above (63 full pages and one of 336, so 64 pages and 65 transactions).
Terminal counts are unchanged for both: 3 pages and 4 transactions for the unit
fixture, 64 pages and 65 transactions for the scope.

What changed: `Result.Pages` no longer counts the cleanup transaction that runs
when the scope row has disappeared mid-rebuild, because that transaction loads
no page. Before the change, the new test
`TestRebuildScopeDisappearedMidRebuildCountsNoCommittedPage` reported
`Pages:2` for one committed page; after, it reports 1. An empty scope now
reports 0 pages and 2 transactions with outcome `cleared`. A second test,
`TestRebuildScopeRepairSkipsAScopeWhoseDirtyRowIsAlreadyGone`, covers the
`skipped` outcome: one start transaction, no rebuild statement.

Observability Evidence: `Pages` feeds only the progress and `scope_failed` log
lines in `package_manifest_consumption_backfill.go`, where it now matches the
documented meaning (committed fact pages). `Transactions`, the duration and
lock-wait histograms, and the outcome counter are unchanged, so every metric an
operator uses at 3 AM reads the same.
