# Settle skip-reason discriminator (#9975 criterion 4, step 2)

Refs #9975. Arbiter ruling 2026-11-26 (fix-design): criterion 4 stays FAIL,
both bars stand; authorized step 1 is the drain discriminator (per-tick
admitted vs deleted vs admitted-not-in-chunk vs in-chunk-version-mismatch,
plus head seq/version trajectory) with read-only replica plus logs first,
and a settle-skip-reason counter if logs cannot split it.

## Step 1 outcome: logs plus replica cannot split the two skip reasons

Read-only QA discriminator window 2026-11-26 ~15:22-15:28Z, replica
`eshu-postgresql-read-0` (`statement_timeout` 30 s, SELECT only, no temp
tables, no writes) and `kubectl logs` on
`eshu-resolution-engine-vulnerability` (all keyed commits run there; the
general engine logged zero keyed commits in the window):

- Dirty-log depth 66,110, oldest `captured_at` 2026-11-25 20:33:18Z (axios
  CVE batch, still the head ~19 h later). Head advances within the frozen
  timestamp: admission-order head seq 40064782 at 15:22:48Z to 40066120 at
  15:27:58Z, while 2,151 rows remain at the frozen minimum capture. No
  single pinned row; a large same-timestamp batch draining slowly.
- Admitted window (oldest 5,000 by `captured_at, seq`): 4,986 distinct
  identifiers (4,985 fact plus 15 scope), version sums 601,923 rising to
  621,765 over six minutes. Per-tick deleted marks (`identifiers` in the
  commit log) median 1,846 over 22 ticks in 30 minutes: about 0.3% of
  admitted marks. Every tick `Bounded`, `chunk_truncated_by`
  `identifiers+scanned_copies`, `chunk_identifiers` 20-39 (about 0.7% of
  admitted identifiers), `remainder_rows_converted` 0 on every tick.
- `converted = 0` falsifies the convert self-bump as the current-window
  suppressor, and external writers stage only (collapse runs before
  admission), so a same-snapshot mismatch needs the convert path. But the
  logs carry no admitted-row count and no skip split (`identifiers` counts
  marks, not rows), and the replica has no admission snapshot: logs plus
  replica bound mismatch at zero only indirectly. The quantitative split of
  admitted-not-in-chunk vs in-chunk-version-mismatch needs the counter.

## What changed

`settleWinnersKeyedDirtyLog`
(`go/internal/storage/postgres/supply_chain_impact_winners_keyed_settle.go`)
reports `winnersSettleSkips{NotInChunk, VersionMismatch}` on the applied
path and zeros on the over-cap fallback (no chunk table there). Both counts
are read-only `SELECT count(*)` over the admitted window joined to the log
by `seq`, survivors of the delete: rows failing the shared applied
predicate count as not-in-chunk, rows passing it count as version-mismatch
(a passing survivor necessarily moved past the admitted version, else the
delete would have removed it). The predicate is one helper
(`winnersKeyedAppliedPredicate`) shared with the drain, pinned by
`TestWinnersSettleSkipPredicateIsSharedWithAppliedDrain`. The result carries
`SettleSkippedNotInChunk` / `SettleSkippedVersionMismatch`, the tick log
carries `settle_skipped_not_in_chunk` /
`settle_skipped_version_mismatch`, and
`eshu_dp_supply_chain_impact_winners_settle_skipped_rows_total{reason}`
counts them (`admitted_not_in_chunk`, `in_chunk_version_mismatch`).

No drain behavior changes: the delete statement, chunk bounds, 120 s guard,
bars, and lock order are untouched; the two counts take no row locks
(`TestSupplyChainImpactWinnersSettleSkipCountsAreReadOnlyLive` asserts the
plans show no `LockRows`, `FOR UPDATE`, or `ModifyTable`).

## No-Regression Evidence

No-Regression Evidence: scratch PostgreSQL 18.6, disposable database via
`winnersSettleGateDB`, real bootstrap schema, commit `462c0a3` tree:

- Correctness: `TestSupplyChainImpactWinnersKeyedSettleSkipSplitLive`
  (12 facts at version 20 plus one scope row, six-identifier chunk, one
  chunked row bumped mid-tick) asserts skips `{6, 1}`, drained marks 101,
  remaining 7, row conservation (13 admitted = 6 deleted + 7 survivors),
  mark conservation (241 admitted = 100 drained + 141 survivor), and a
  staged re-mark folded between passes merging into the survivor
  (depth 7, versions 142). The pre-existing re-mark live test now also
  asserts skips `{0, 1}` through the production capture plus collapse path.
  Zero `40P01`: the deadlock-free settle gate still passes unchanged.
- Cost (throwaway probe, same harness, file removed after the run):
  `EXPLAIN (ANALYZE, BUFFERS, TIMING OFF)` of both counts over a full
  5,000-row admitted window with a 200-identifier chunk: 3.7 ms
  (not-in-chunk) and 3.4 ms (version-mismatch). With a 66,000-row log (QA
  depth), 23.1 ms for the not-in-chunk count (the mismatch count shares the
  join shape, 3.4 ms against 3.7 ms at 5k depth), so both counts cost tens of
  ms per tick — about 1% of a 4 s p50 keyed tick. The plan is a log seq scan plus 5,000-66,000 consumed-PK
  probes plus 200-row chunk `EXISTS` subplans, the same shape as the
  existing per-tick remaining-depth count; the dirty log is narrow (66k
  rows), not the 579 GB fact heap, and both counts run under the tick's
  120 s statement guard like every other tick statement.

Observability Evidence: `TestResweepWinnersKeyedRecordsSettleSkippedByReason`
asserts the result fields and the per-reason counter values end to end
through `ResweepWinnersKeyed`; the winners telemetry page
(`docs/public/reference/telemetry/supply-chain-impact-winners.md`) documents
the new counter and the operator read (dominant `admitted_not_in_chunk`
means the chunk bound holds the drain; rising `in_chunk_version_mismatch`
with a rising converted rate names convert self-bump).

Out of scope for this step: the contingent settle fix itself (not
implemented), keyed-vs-full parity (no settle behavior changed), QA
citable re-read (coordinator-owned, needs a rolled digest).
