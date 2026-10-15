# Deferred Backfill Fan-In: Skip Unchanged Partitions (#8082)

## Change

The whole deferred-maintenance pass no longer republishes readiness and the
partition memo for a partition the pass did not change. A partition is left
untouched when:

1. it was a memo hit at pass start (same catalog fingerprint), so its fact load
   was skipped;
2. the pass inserted zero evidence rows under it
   (`ExcludePartitionsWithNewEvidence`);
3. its (scope, generation) still matches the fact-load snapshot (the batch
   guard); and
4. its `cross_repo_evidence` / `backward_evidence_committed` row exists. One
   batched `ListPartitionsMissingReadinessQuery` read checks this. A missing
   row is republished.

Every other partition publishes as before, under the same sorted locks in its
own transaction. The partition-scoped pass (#7584) still calls
`PublishDeferredBackfillPartitions` with no unchanged set, and its behaviour is
unchanged. A failed evidence batch still withholds the whole fan-in. A failed
readiness check falls back to publishing every partition.

Republishing an unchanged partition rewrote only phase `committed_at` and
`updated_at` and memo `committed_at`, with the same fingerprint. No reader
decides on those timestamps. `GraphProjectionPhaseStateStore.Lookup`,
`activation` `phaseReadyQuery`, and the obligation backlog all test for the row.
The full invariant, including the pending-generation same-ID retry case, is in
the doc comment on `splitUnchangedDeferredPartitions`
(`go/internal/storage/postgres/backfill/fanin_unchanged.go`).

## Performance Evidence:

Shim: Postgres 18.6, database `p8082`, 809 partitions, 740 memo hits, 8 fan-in
workers. The bench harness replays the fan-in transaction statements. Runs come
from the coordinator's shim report.

| Fan-in shape | Wall time |
| --- | --- |
| Full fan-in (main) | 4.756 s and 4.811 s (two runs) |
| Without the ArgoCD probe | 0.112 s and 0.156 s |
| Memo-hit partitions only (740) | 4.0 s |
| Only the 69 non-hit partitions (this change) | 0.38 s (-91%) |

The ArgoCD-bearing probe (`ListArgoCDBearingPartitionsQuery`) is 97.3 to 97.4
percent of summed transaction time (36.9 to 37.3 s over 809 calls, mean
46 ms). Every other fan-in statement totals about 0.1 s.

The new readiness check, `EXPLAIN (ANALYZE, BUFFERS)` on the same shim over the
740 memo-hit partitions: a Nested Loop Anti Join with 740 index-only probes of
`graph_projection_phase_state_pkey`, 0 heap fetches, 1,459 shared hits, and
8.0 ms of execution (3.0 ms of it builds the input arrays). This cost replaces
740 probe transactions.

QA log facts before the change: `deferred_backfill_fanin_completed
partitions=809 published=809 skipped=0`, `unskipped_new_evidence=0`, and a
fan-in of 14.5 to 15.7 s. Expected after: about 69 partitions published, about
740 skipped as `unchanged_since_memo`, and `readiness_rows` near 69.
NOT_CHECKED: QA wall time and QA payload bytes after the change.

Concurrency: unchanged partitions take no advisory lock and open no
transaction (`TestDeferredFanInUnchangedPartitionsOpenNoTransaction`).
Published partitions keep one transaction each, sorted repository locks, and
the worker bound. Conflict domain: the per-repository deferred-maintenance
advisory lock. Worker and lease settings are unchanged
(`ESHU_DEFERRED_BACKFILL_CONCURRENCY`, default `min(NumCPU, 8)`).

Proofs:

- `TestDeferredBackfillFanInSkipsUnchangedMemoHitPartitionsLive`: RED on main.
  The unchanged partition's phase `updated_at` moved to the pass clock. GREEN
  after the change.
- `TestDeferredBackfillFanInRepublishesUnchangedPartitionMissingPhaseRowLive`:
  self-healing republish.
- `TestTargetedMaintenanceMatchesWholePass`: each fixture now declares the
  partitions the whole arm leaves unchanged. The whole arm must hold those
  phase and memo rows identical to before. The arms may differ there only in
  timestamps. `TestTargetedDiffUnchangedToleranceSeededViolations` is the
  seeded-violation pair for that comparator.

## Observability Evidence:

- `eshu_dp_deferred_backfill_fanin_skipped_total{reason="unchanged_since_memo"}`
  counts unchanged partitions per pass. The label set stays closed at three
  values.
- New log line `deferred_backfill_fanin_unchanged_checked candidates=N
  skipped_unchanged=S republished_missing_phase=M`, and
  `deferred_backfill_fanin_unchanged_check_failed ... falling_back=true` on a
  failed check. The pinned `deferred_backfill_fanin_completed` line keeps its
  shape. Its `skipped` now includes the unchanged partitions.
- The `relationship.backfill.fanin` block gains `skipped_unchanged`. The pass
  span gains `fanin_skipped_unchanged_count` and
  `fanin_unchanged_missing_phase_count`.
- `readiness_rows` on `deferred_backfill_completed` and on the `write_batches`
  block counts the readiness rows written this pass, not every active
  partition.
