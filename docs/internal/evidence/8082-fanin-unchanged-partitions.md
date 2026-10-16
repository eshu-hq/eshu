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
   guard), and its scope is still on that generation when the fan-in checks;
   and
4. its `cross_repo_evidence` / `backward_evidence_committed` row exists.

One batched `ListUnchangedPartitionsNeedingPublicationQuery` read checks 3 and
4. A missing readiness row is republished. A scope that moved to a new
generation after its batch goes to the locked path, whose under-lock re-read
declines it with `generation_advanced_since_batch`, as before. A move that
commits after the unlocked check is counted `unchanged_since_memo`. Neither
case writes a row for the superseded generation.

Every other partition publishes as before, under the same sorted locks in its
own transaction. The partition-scoped pass (#7584) still calls
`PublishDeferredBackfillPartitions` with no unchanged set, and its behaviour is
unchanged. A failed evidence batch still withholds the whole fan-in. A failed
readiness check falls back to publishing every partition.

Republishing an unchanged partition rewrote only phase `committed_at` and
`updated_at` and memo `committed_at`, with the same fingerprint. Readiness
readers test that the phase row exists (`GraphProjectionPhaseStateStore.Lookup`,
`activation` `phaseReadyQuery`, the obligation backlog). One reader orders on
memo `committed_at`: the partition-scoped pass's `targetedCatalogBaselineQuery`
picks each scope's latest memo (`owed_latest_memo`). That order still holds.
Memos are written only for the scope's active generation under its lock, and a
superseded generation never becomes active again (#7130). So a newer
generation's first memo commits after every memo of an older one, and
`committed_at` orders a scope's memos by activation, not by observation
recency. Skipping a re-stamp keeps a memo at its first publication.
`TestWholePassFanInKeepsTargetedBaselineOnActiveMemoLive` proves the active
generation's memo still wins after a skipped re-stamp.
The full invariant, including the pending-generation same-ID retry case, is in
the doc comment on `splitUnchangedDeferredPartitions`
(`go/internal/storage/postgres/backfill/fanin_unchanged.go`).

## Performance Evidence:

Shim: Postgres 18.6 (aarch64 container on the development host), 809
partitions, 740 memo hits, 6 ArgoCD-bearing memo misses, 8 fan-in workers.

Built code. A throwaway harness (not committed) called the shipped
`publishChangedDeferredBackfillPartitions` with the real
`GraphProjectionPhaseStateStore`, on `p8082_timing`, a `CREATE DATABASE ...
TEMPLATE p8082` copy of the shim. Inputs came from the shipped loaders:
contributions from `LoadActiveRepositoryGenerations` (809 partitions), the
snapshot from `LoadActiveScopeGenerationPartitions`, and the unchanged set from
the 740 memo rows. The runs alternated after one warm-up of each arm (the cold
warm-up of the before arm took 17.135 s).

| Fan-in, 8 workers | Run 1 | Run 2 | Run 3 |
| --- | --- | --- | --- |
| Before (unchanged set nil, publishes 809) | 4.913 s | 4.888 s | 5.362 s |
| After (740 unchanged, publishes 69) | 0.478 s | 0.438 s | 0.481 s |

The median falls from 4.913 s to 0.478 s (-90%). Every after run logged
`deferred_backfill_fanin_unchanged_checked candidates=740 skipped_unchanged=740
republished_missing_phase=0 generation_advanced=0` and `published=69
skipped=740`.

Statement replay (the coordinator's shim report, before the change was built):

| Fan-in shape | Wall time |
| --- | --- |
| Full fan-in (main) | 4.756 s and 4.811 s (two runs) |
| Without the ArgoCD probe | 0.112 s and 0.156 s |
| Memo-hit partitions only (740) | 4.0 s |
| Only the 69 non-hit partitions (projection of this change) | 0.38 s |

The ArgoCD-bearing probe (`ListArgoCDBearingPartitionsQuery`) is 97.3 to 97.4
percent of summed transaction time (36.9 to 37.3 s over 809 calls, mean
46 ms). Every other fan-in statement totals about 0.1 s.

The shipped check, `ListUnchangedPartitionsNeedingPublicationQuery`, ran
under `EXPLAIN (ANALYZE, BUFFERS) EXECUTE` of a prepared statement with the
740 memo-hit partitions. It ran on `p8082_plan`, a `TEMPLATE p8082` copy whose
phase table was padded with extra keyspace rows. Times are the warm second and
third runs.

| Phase table rows (heap) | Readiness plan | Execution |
| --- | --- | --- |
| 810 (592 kB) | hashed SubPlan over one Seq Scan | 2.8 ms |
| 97,900 (13 MB) | hashed SubPlan over one Seq Scan | 7.6 ms |
| 214,408 (27 MB) | hashed SubPlan over one Parallel Seq Scan | 9.8 to 10.1 ms |
| 1,165,890 (149 MB) | 740 index-only probes of `graph_projection_phase_state_pkey`, 0 heap fetches | 4.9 to 5.0 ms |

The readiness test sits in the select list as `NOT EXISTS`, so Postgres
chooses between a hashed and a per-row subplan by cost. It is not an anti
join. The generation probe is 740 index-only scans of
`scope_generations_scope_latest_lookup_idx` at every size. QA's phase heap is
about 29 MB, next to the 27 MB row. At that size the check costs about 10 ms
per pass, against about 4.4 s it removes, so it is not a performance problem.
Its cost is included in the after timings above.

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

- `TestWholePassFanInSkipsUnchangedMemoHitPartitionsLive`: RED on main.
  The unchanged partition's phase `updated_at` moved to the pass clock. GREEN
  after the change.
- `TestWholePassFanInRepublishesUnchangedPartitionMissingPhaseRowLive`:
  self-healing republish.
- `TestWholePassFanInKeepsTargetedBaselineOnActiveMemoLive`: the targeted
  catalog baseline still picks the active generation's memo.
- `TestDeferredFanInUnchangedPartitionWithAdvancedGenerationKeepsAdvancedReason`:
  RED before the fix (counted `unchanged_since_memo`); the partition now keeps
  `generation_advanced_since_batch`.
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
  skipped_unchanged=S republished_missing_phase=M generation_advanced=G`, and
  `deferred_backfill_fanin_unchanged_check_failed ... falling_back=true` on a
  failed check. The pinned `deferred_backfill_fanin_completed` line keeps its
  shape. Its `skipped` now includes the unchanged partitions.
- The `relationship.backfill.fanin` block gains `skipped_unchanged`. The pass
  span gains `fanin_skipped_unchanged_count` and
  `fanin_unchanged_missing_phase_count`.
- `readiness_rows` on `deferred_backfill_completed` and on the `write_batches`
  block counts the readiness rows written this pass, not every active
  partition.
- `deferred_backfill_skip_set_revised` is now logged after the fan-in
  succeeds, so a pass whose fan-in fails no longer reports it.
