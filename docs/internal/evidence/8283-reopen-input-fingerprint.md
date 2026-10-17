# Reopen Input Fingerprint For deployable_unit_correlation

Issue #8283. The deferred-maintenance pass replayed every succeeded
`deployable_unit_correlation` item above the replay floor on every shard drain.
This change skips an item whose inputs have not changed and keeps the other
three correlation domains on the blanket replay.

## Problem, as measured

One steady-state wave on the production environment reopened 799
`deployable_unit_correlation` items, 232 `aws_cloud_runtime_drift` items and
166 `container_image_identity` items, with `skipped_by_memo=0` for all three.
All 799 correlation items passed every readiness gate on their first claim (0
`reducer execution deferred` lines, 799 successes). A listing filter on the
readiness gates would therefore have excluded 0 of 799, and it is unsafe: the
corpus fence and the canonical-readiness gate are corpus-wide, and the same
transaction reopens `deployment_mapping` first, so a changed item could stay
un-reopened if that pass is the last one. A timestamp rule
(`activated_at > updated_at`) is racy: `updated_at` is stamped at ACK and
`activated_at` by another clock before its commit. This change builds neither.

## Design

A per-item input fingerprint, taken in the fused fenced read and stored in a
side table. A missing row reopens the item.

- Table `reducer_work_input_fingerprints` (migration 192, Go DDL in
  `backfill/input_fingerprint_store.go`): `work_item_id` primary key with a cascading
  foreign key to `fact_work_items`, `domain`, `repo_ids`, `fingerprint`,
  `recorded_at`. Open PRs hold migrations 188 to 191, so this one is
  192.
- The handler writes the row (idempotent upsert) after its graph writes and
  before the ACK, following the admission-decision writer pattern.
- The fingerprint is computed inside
  `backfill.ListResolvedByReposWithCorpusFenceAndInputFingerprintSQL`, the fused
  fence-and-read statement with one more materialized CTE, so the fence
  verdict, the rows and the fingerprint share one statement snapshot.
- One shared SQL definition (`backfill.InputFingerprintExprSQL`) is embedded by
  both that statement and `backfill.ListSucceededDeployableUnitWorkItemsQuery`.
  The listing recomputes the value from the stored `repo_ids` and the work
  item's generation. Go never hashes and never compares a Go value with a SQL
  value.
- The listing returns every succeeded item above the replay floor with an
  `unchanged` flag (a flag, not a filter), so the pass can count what it skipped.

### Deviations from the arbiter ruling

- SHA-256 instead of md5. The production database runs FIPS-mode OpenSSL:
  `SELECT md5('probe')` on the production reader fails with `could not compute
  MD5 hash: unsupported`, and `sha256()` works. A hermetic test bans `md5(`.
- The fingerprint carries per-generation resolved-row counts, not only
  `(generation_id, activated_at)`. The ruling's invariant "resolution inserts
  resolved rows and activates in one transaction" is false in source:
  `cross_repo_resolution.go` inserts rows one autocommit statement at a time
  (step 4, `ON CONFLICT DO NOTHING` into the scope's own generation id) and
  activates later (step 6). A replay can add rows to an active generation
  before `activated_at` moves, and if the resolution item dies in between no
  stamp ever follows. Counts only grow within a generation, so they close that
  window. `TestInputFingerprintReopenRowsAddedToActiveGenerationWithoutRestamp`
  fails without them. The counts are only a sound signal while
  `resolved_relationships` is append-only, so `TestResolvedRelationshipsStayAppendOnly`
  enumerates every non-test Go source and migration and fails on any UPDATE,
  DELETE, TRUNCATE, DROP, trigger, rule or foreign key that could change or
  remove a row (a seeded-violation pair plants each shape and fails it).
- Migration 192, not 188: open PRs hold 188 to 190 and 191.

## Local proof

Disposable PostgreSQL 18.6 on a private native cluster, real bootstrapped
schema, one isolated schema per test.

Observability Evidence: the pass reports the skip three ways, none of which
carries a scope or repository id:

- span `relationship.backfill.reopen` and its completion log gain the
  `skipped_unchanged` attribute (always 0 for the other domains);
- the structured line is now
  `reducer_work_items_reopened domain=... count=N skipped_unchanged=M`;
- counter `eshu_dp_correlation_reopen_skipped_unchanged_total{domain}` sits next
  to `eshu_dp_correlation_reopened_total{domain}`, which now counts only the
  items actually reopened.

A deploy that records fingerprints shows `skipped_unchanged` rising on a quiet
corpus. If it stays 0, the handler is not recording or the table is empty.

### Behavior cases (`input_fingerprint_reopen_live_test.go`)

| Case | Expected | Kills |
| --- | --- | --- |
| unchanged item | stays succeeded; also proves recorded equals listing-side | listing that always reopens |
| missing row | reopens | skip on missing row |
| foreign activation stamped before the item's `updated_at` | reopens | `activated_at > updated_at` |
| foreign scope re-activated in place | reopens | own-scope-only check |
| foreign relationship generation superseded, no replacement | reopens | `activated_at`-only check |
| unrelated foreign scope re-ingested | stays succeeded | corpus-wide clock |
| own relationship generation re-activated | reopens | |
| deployment repository re-ingested | reopens | |
| rows added to an active generation with no re-stamp | reopens | fingerprint without counts |
| claimed item, foreign activation, ACK, next pass | reopens | |
| listing failure | pass fails, that pass's deployment_mapping reopen rolls back, next pass reopens both | silent skip |

Mutants run against the listing SQL and restored afterwards (`diff` against the
original is empty):

- the fingerprint replaced by `NOT EXISTS (active generation touching the stored
  repo ids with activated_at > work.updated_at)`: seven of the cases above fail;
- the fingerprint without row counts: the rows-added case fails;
- a missing row treated as unchanged: the missing-row case fails.
- the own half of the fingerprint, isolated by a candidate set no resolved row
  touches (`TestInputFingerprintReopenOwnGenerationOnlySignals`: rows added to
  the own generation with no re-stamp, an own status change with the same
  `activated_at`, a bare re-stamp, and an unchanged control): dropping the whole
  own part, `own.status`, the own row count, or `own.activated_at` each fails
  at least one case.
- the append-only guard: changing `insertResolvedSQL` from `DO NOTHING` to `DO
  UPDATE SET ...`, or planting a `MERGE INTO resolved_relationships ... WHEN
  MATCHED THEN UPDATE` in a non-test Go file, turns
  `TestResolvedRelationshipsStayAppendOnly` red; the clean tree is green. The
  guard is a text scan, so a table name built at runtime would pass it.
- a compile-time assertion in `cmd/reducer` ties `*postgres.RelationshipStore`
  to `reducer.InputFingerprintedResolvedRelationshipLoader`, so a signature
  drift fails to compile instead of silently turning the gate off.

### Interleavings (`input_fingerprint_interleave_live_test.go`)

Run with the real `ReducerQueue` claim and ACK, the real fingerprint store, the
real `ReopenSucceeded`, and `reset.ApplyPreRetirement`, on separate connections.
All pass, and the new live tests also pass under `-race`.

- pass A lists the item as changed, pass B reopens it, a worker claims it, then
  pass A runs its reopen UPDATE: it affects 0 rows (status guard, no double
  run), the lease and `attempt_count` are untouched, the handler's write and
  the ACK follow, the next pass leaves it succeeded, and a later foreign change
  reopens it (no lost reopen). Removing `AND status = 'succeeded'` from the
  reopen UPDATE makes this test fail;
- a foreign activation commits while the item is claimed: the pass leaves it
  alone, the pass after the ACK reopens it;
- an activation held uncommitted while the fingerprint is read: the read
  snapshot excludes it, and after the commit the recorded value no longer
  matches;
- 48 concurrent writers on distinct items under `lock_timeout = 3s` record and
  update without a lock timeout or deadlock;
- a retry after a rejected ACK upserts one row with the newer value;
- an uncommitted fingerprint insert (foreign-key check takes `KEY SHARE` on the
  work row): the ACK's non-key UPDATE completes at once, and the reset path's
  DELETE waits, then cascades the fingerprint row away.

The existing tests the issue cites stay green:
`ingestion_reopen_correlation_maintenance_test.go` (all four tests) and
`ingestion_reopen_correlation_failed_generation_test.go`.

## Performance

Performance Evidence: `EXPLAIN (ANALYZE, BUFFERS)` on the production reader,
read-only (`default_transaction_read_only=on`, `statement_timeout=60000`).
Production holds 1,558 relationship generations (802 active) and 2,544 resolved
relationship rows; the reopen listing returns 798 items, each with one
repository id. The side table does not exist in production, so the listing was
run with the stored `repo_ids` supplied by a `VALUES` stand-in carrying the
real repository ids and a dummy fingerprint; the expression, the joins and the
floor are the shipped text. Nine runs per variant, interleaved with alternating
first mover. Medians, execution time:

| Listing | Median | Min | Max |
| --- | --- | --- | --- |
| current blanket listing | 35.2 ms | 33.1 | 37.3 |
| fingerprint listing, every item has a row (steady state) | 132.7 ms | 130.8 | 137.3 |
| fingerprint listing, no rows (first pass after the roll) | 36.2 ms | 34.5 | 39.7 |

The steady-state listing adds about 97 ms per pass against the 500 ms budget.
Treat 132.7 ms as an upper bound: the `VALUES` stand-in has no key, so the plan
shows a 798 by 798 join filter, while the real table joins on its primary key.
It saves one reopen UPDATE and one handler run for each unchanged item. The
fingerprint is evaluated only for items that have a row (`CASE`), and the first
pass after the roll costs the same as today.

The fingerprint adds one more scan to every handler run through the fused
read. Seven interleaved runs each on the production reader:

| Repository set | Plain fused read | With fingerprint |
| --- | --- | --- |
| median repository (a few touching rows) | 10.10 ms | 9.74 ms |
| heaviest repository (315 touching rows) | 11.61 ms | 13.50 ms |
| three heaviest repositories | 13.88 ms | 16.25 ms |

The fence dominates the fused read (about 9 ms), so the fingerprint adds 2.4 ms
at most on the heaviest repository sets.

No-Regression Evidence: the conflict domain is one work item row. Writers
touch distinct rows (the side table's primary key); the ACK's non-key UPDATE is
compatible with the foreign-key `KEY SHARE` lock; the reopen UPDATE is
status-guarded so a stale pass cannot reopen a claimed item. No worker count,
batch size or lease setting changes, and nothing is serialized. Progress,
blocking and failure show in `skipped_unchanged`, the existing
`eshu_dp_correlation_reopened_total`, and the `reducer_work_items_reopened` line.

## Not checked

- The deployed wave. After the rollout the same wave trigger should show
  `reopened` falling and `skipped_unchanged` rising; a forced-change canary
  (re-ingest one repository whose deployment_mapping re-resolves) should reopen
  its items.
- The share of items with no workload candidates. They never record a
  fingerprint and keep reopening every pass.
- The partition-scoped targeted pass keeps its blanket correlation reopen.
- Candidate repository ids were approximated from each scope's repository facts
  for the production measurement; the handler uses the candidate extractor's ids.
- `container_image_identity`, `kubernetes_correlation_materialization` and
  `aws_cloud_runtime_drift` keep the blanket reopen. The drift tail belongs to
  #7666 and #8203.

## Rollout

Apply migration 192 with the schema Job before the reducer image rolls, not
only before the ingester.

- Ingester: if the binary rolls first, the listing fails on the missing table
  and the whole maintenance reopen transaction rolls back, losing that pass's
  deployment_mapping and code_import_repo_edge reopens.
- Reducer: if the binary rolls first, `recordInputFingerprint` fails on the
  missing table after the graph writes and the phase publish. The wrapped
  Postgres error carries no class from `nonCountingReducerRetryFailureClasses`
  (`reducer_queue_readiness_sql.go`), so the attempt takes the counting retry
  path and a deployable_unit_correlation item that has candidates can reach
  dead letter. A dead-lettered item does not come back through the reopen,
  which lists succeeded items only.

The first pass after the roll finds no rows and reopens every item once.
Rolling the image back is safe; the table is additive.

## Accepted risk: graph-side narrowing

The blanket reopen also repaired graph-only losses: an item whose result row
was fine but whose graph edges were lost got rewritten on every drain. A
skipped item is not rewritten. This is accepted. A concrete instance: a stalled
zombie worker B (lease expired, heartbeat not yet failed,
`service_heartbeat.go`) can write a stale graph between worker A's graph write
and A's fingerprint record. The order is A_graph, B_graph, B_record, A_record.
A's current fingerprint wins and the item stays skipped on a stale graph. The
record write is not the place to fix this; it is a statement of accepted risk,
not a code change.
