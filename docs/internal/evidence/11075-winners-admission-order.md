# Winners admission by capture time (#11075)

Refs #11075 and #10972. Audit row 11 of the re-run contract ("R freeze").

## What changed

`admitWinnersKeyedWindowSQL`
(`go/internal/storage/postgres/supply_chain_impact_winners_keyed_sql.go`) admits
the keyed tick's dirty-log window `ORDER BY captured_at, seq` instead of
`ORDER BY seq`. The window size (5,000 rows) and the identifier chunk bound (200)
are unchanged.

## Why

The watermark horizon is `min(captured_at)` of the remainder minus 1 us
(`selectWinnersKeyedRemainingMinSQL`, `horizonCandidate`). The scope-remainder
convert (`convertWinnersKeyedScopeRemainderSQL`) keeps the old `captured_at`
on a new tail `seq`, or lowers a queued row's `captured_at` with `LEAST` and
keeps that row's `seq`. With FIFO-by-`seq` admission, the oldest capture sat
behind every newer row, so the watermark could not pass it until the window
reached it. On the QA environment the watermark stayed at 18:04:45 while ticks
kept consuming newer rows.

The QA environment replica read (2026-11-24, 01:25Z) shows the shape: depth
56,698. The oldest capture (18:04:45.199694) is on seq 32,360,904 with 6,961
rows ahead of it, and 443 activation remainder rows with the same capture sit
at seq 32,826,510-32,827,489. The shipped 5,000-row window ran from 19:08:42 to
20:19:20 and held none of the oldest rows. The proposed window starts at
18:04:45, and 1,111 of its rows lie past the shipped window's last seq.

## Readers and writers of seq order (audit)

Nothing else relies on the window being the lowest seqs:

- The consumed table and its `seq` index serve the applied delete's join only
  (`indexWinnersKeyedConsumedSeqSQL`, `deleteWinnersKeyedAppliedDirtySQL`
  joins `c.seq = d.seq`). The join is by row identity, not order.
- The settle drains lock in the conflict-key order
  (`winnersDirtyKeyOrderClause`: kind, the fact five-tuple, the scope pair, then
  `seq`), independent of admission order.
- The identifier chunk already orders by `MIN(captured_at)`, tie-break
  `MIN(seq)` (`buildWinnersKeyedIdentsSQL`, `selectWinnersKeyedChunkIdentsSQL`,
  #10706). Window and chunk now use the same order.
- The version recheck (`d.dirty_version = c.dirty_version`) and the over-cap
  delete (`deleteWinnersKeyedAllDirtySQL`) act on admitted rows by identity.
- A row that commits after admission is not in the admission snapshot under
  either order. The documented late-commit one-tick exception is unchanged.
- The staging collapse is bounded by `seq` on the staging table
  (`supply_chain_impact_winners_staging_collapse.go`), not on the log, and is
  unchanged. Its leftovers stay in the horizon through `stagingUnionRemaining`.
- The audit (`createWinnersAuditPendingSQL`), the audit sweep, the NVD trip
  sampler, and the full rebuild's residual read scan the log by content or
  `min(captured_at)`, never by window position.

## Performance

Performance Evidence: QA environment read replica, `EXPLAIN (ANALYZE, BUFFERS,
SETTINGS)` of the admission SELECT arm, two runs each, depth 56,698, work_mem
64MB (a read-only shim the coordinator ran; the INSERT is not measured, see below):

| Admission order | Plan | Execution time | Buffers |
| --- | --- | --- | --- |
| `seq` (shipped) | Index Scan on the `seq` primary key, Limit 5000 | 4.062 ms, about 3.8 ms | 19,581 hit |
| `captured_at, seq` | Parallel Seq Scan, top-N heapsort (about 2.5 MB per worker, in memory), Gather Merge, Limit 5000 | 22.179 ms, 20.762 ms | 4,581 hit |

The decision threshold was set before the run: at most 250 ms on both runs with
an in-memory sort, or no change without an index. The reorder adds about 18 ms
to a tick that averages about 91 s on the QA environment (21 s minimum). That
is under 0.1% of the average tick. No index was added: a btree on
`(captured_at, seq)` would add one entry per log insert on a delete-heavy queue,
and it would make the convert's `LEAST` updates non-HOT, all to save about
18 ms. hypopg is not installed on the replica.

The INSERT into the consumed temp table cannot run on a hot standby. It copies
5,000 rows under either order, so the SELECT arm is the whole difference.

## Proof

- `TestWinnersKeyedAdmissionOrdersByCapturedAtThenSeq` (hermetic) reads the
  ORDER BY of the shipped constant. It failed on `seq` (commit 572d83a4f1) and
  passes on `captured_at, seq`.
- `TestSupplyChainImpactWinnersResweepKeyedAdmitsTheOldestCapturedAtFirstLive`
  (live, reducer-contention gate) seeds the oldest capture on the tail seq
  behind three 2-row windows. The first tick must apply it and move the
  watermark past it. The drain must consume every row exactly once, oldest
  first, keep the watermark monotone and above no queued row, and end at
  full-recompute parity. It was written to fail on the shipped order and pass
  on the new one. The coordinator runs it against disposable PostgreSQL.

## Observability

No-Observability-Change: no instrument, label, span attribute, or log key is
added. The change is visible on the existing gauges.
`eshu_dp_supply_chain_impact_winners_dirty_log_oldest_age_seconds` and
`eshu_dp_supply_chain_impact_winners_watermark_age_seconds` climbed one second
per second during the freeze. With the oldest capture admitted first they fall
or hold while ticks run. `eshu_dp_supply_chain_impact_winners_tick_phase_duration_seconds{phase="consume"}`
carries the admission cost (about 18 ms more per tick). The operator read is
documented in `docs/public/reference/telemetry/supply-chain-impact-winners.md`.

## Not changed

The identifier chunk bound stays 200. The cost-per-kept-row recalibration
(about 150 kept rows per identifier on the QA environment against the 13 the
bound was calibrated on) is separate work.
