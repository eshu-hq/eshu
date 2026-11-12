# #9805 chunked consume: keyed-apply linearity at 400/1000/4000 identifiers

Refs #9805. Arbiter-required linearity probe for the chunked consume: the
keyed apply must scale with the post-expansion identifier count, so that
bounding the identifiers per tick bounds the apply. Measured live at this
head; the commit-time numbers (31.6ms at 400, 74.5ms at 1000, 281.7ms at
4000) are recorded for comparison only and nothing below is copied from them.

## Method

- Harness: scratch `TestScratchChunkLinearityLive` at head `c2228c5801`,
  deleted after the run. Production path throughout: the production impact
  writer seeds the corpus, a full resweep stores it, each round rewrites
  through the writer, `ExplainWinnersKeyedForTest` captures the
  `EXPLAIN (ANALYZE, BUFFERS)` plans of the tick's `active_facts` build and
  apply statement (rolled back), then one timed `ResweepWinnersKeyed` with
  `ChunkMaxIdentifiers` set to the point runs the real tick.
- Corpus: one scope, 5,000 findings with distinct CVEs, ten keys per package
  triple: 5,000 stored winners after the full resweep, one kept fact per
  identifier. Each round carries the full finding set (the writer pass is a
  replace set: findings outside the write set are tombstoned) and bumps only
  the first `m` priorities, so exactly `m` rows go dirty per point.
- Backend: disposable PostgreSQL 18.6 (Alpine, aarch64 musl) in the lane
  container `eshu-9805-chunk-pg` on `127.0.0.1:55441` (`postgres`/`test`),
  one database for all three points in sequence.
- Cache: warm. Each point reports the second of two consecutive `EXPLAIN`s.
- Heap: `VACUUM supply_chain_impact_canonical_winners` after each rewrite, so
  every point sees the same 5,000 live winner rows. The plan shape and the
  planner statistics stay as autovacuum left them.
- Host: shared lane host, `/proc/loadavg` 1.14–1.71 during the run (the
  harness `hostLoad()` helper shells `sysctl` and reports `unknown` on
  Linux, so the load below is from `/proc`).
- Date: 2026-11-12.

## Result: the apply is linear in identifiers

| Dirty identifiers | Apply `EXPLAIN (ANALYZE, BUFFERS)` execution | Timed keyed tick | Deleted-node buffers |
| --- | --- | --- | --- |
| 400 | 35.941 ms | 73.7 ms | 338 shared hit |
| 1000 | 88.192 ms | 167.9 ms | 388 shared hit |
| 4000 | 371.061 ms | 994.9 ms | 563 hit + 62 read |

Slope between the points: (88.192 − 35.941) / 600 ≈ 87us per identifier,
(371.061 − 35.941) / 3600 ≈ 93us per identifier. The apply scales linearly
at about 90us per identifier with a few milliseconds of fixed cost (planning
stays under 1ms at every point). The timed tick, which adds admission, the
identifier build, the `active_facts` probe, the tally and the commit around
the same apply, follows the same line (73.7ms / 167.9ms / 994.9ms). Every
timed tick drained its point (`Bounded=false`, `Remaining=0`) and the table
equaled a fresh recompute afterwards.

## The deleted node

At every point the deleted CTE is a `Delete` over a `Hash Anti Join` over a
`Hash Semi Join` over a `Seq Scan` of `supply_chain_impact_canonical_winners`
(5,000 rows), actual 1.4ms, 2.1ms and 5.4ms. Its buffers (338, 388, 563 hit
plus 62 read at 4000) scale with the stored table, not the chunk:
extrapolated to the QA stored count (~71k) that is tens of milliseconds, far
below the narrow (B)-trigger's 1.5s, so no winners-table index is added. The
4000-point reads are heap pages the rewrite rounds extended; a steady-state
vacuumed table serves them from shared buffers, as the 400 and 1000 points
show. No plan change across the points: the same nodes, the same join order.

## Deleted-node excerpts

Verbatim from the three measured plans (full plans:
[9805-chunked-consume-linearity-plans.md](9805-chunked-consume-linearity-plans.md)):

At 400 identifiers:

```text
  CTE deleted
    ->  Delete on supply_chain_impact_canonical_winners w_1  (cost=17.03..486.39 rows=169 width=68) (actual time=1.447..1.447 rows=0.00 loops=1)
          Buffers: shared hit=338, local hit=4
          ->  Hash Anti Join  (cost=17.03..486.39 rows=169 width=68) (actual time=1.446..1.447 rows=0.00 loops=1)
                Hash Cond: (w_1.canonical_key = n_1.canonical_key)
                Buffers: shared hit=338, local hit=4
                ->  Hash Semi Join  (cost=17.00..484.22 rows=169 width=44) (actual time=0.095..1.167 rows=400.00 loops=1)
                      Buffers: shared hit=338, local hit=4
                      ->  Seq Scan on supply_chain_impact_canonical_winners w_1  (cost=0.00..391.99 rows=5399 width=198) (actual time=0.004..0.561 rows=5000.00 loops=1)
```

At 1000 identifiers:

```text
  CTE deleted
    ->  Delete on supply_chain_impact_canonical_winners w_1  (cost=48.60..558.07 rows=156 width=68) (actual time=2.090..2.090 rows=0.00 loops=1)
          Buffers: shared hit=388, local hit=16
          ->  Hash Anti Join  (cost=48.60..558.07 rows=156 width=68) (actual time=2.090..2.090 rows=0.00 loops=1)
                Hash Cond: (w_1.canonical_key = n_1.canonical_key)
                Buffers: shared hit=388, local hit=16
                ->  Hash Semi Join  (cost=48.50..556.00 rows=156 width=44) (actual time=0.220..1.400 rows=1000.00 loops=1)
                      Buffers: shared hit=388, local hit=16
                      ->  Seq Scan on supply_chain_impact_canonical_winners w_1  (cost=0.00..437.93 rows=4993 width=198) (actual time=0.014..0.588 rows=5000.00 loops=1)
```

At 4000 identifiers:

```text
  CTE deleted
    ->  Delete on supply_chain_impact_canonical_winners w_1  (cost=194.59..873.64 rows=148 width=68) (actual time=5.353..5.355 rows=0.00 loops=1)
          Buffers: shared hit=563, local hit=2 read=62
          ->  Hash Anti Join  (cost=194.59..873.64 rows=148 width=68) (actual time=5.353..5.353 rows=0.00 loops=1)
                Hash Cond: (w_1.canonical_key = n_1.canonical_key)
                Buffers: shared hit=563, local hit=2 read=62
                ->  Hash Semi Join  (cost=194.00..871.19 rows=149 width=44) (actual time=0.921..2.694 rows=4000.00 loops=1)
                      Buffers: shared hit=563, local hit=2 read=62
                      ->  Seq Scan on supply_chain_impact_canonical_winners w_1  (cost=0.00..610.71 rows=4771 width=198) (actual time=0.036..0.717 rows=5000.00 loops=1)
```

## QA-anchor calibration

Accepted anchors from
[9805-suppression-expiry-and-tick-shim-20261110.md](9805-suppression-expiry-and-tick-shim-20261110.md):
about 71,000 stored winners, pre-chunk apply p95 4.04s at about 865 keys p95,
about 13 kept facts per identifier, about a 10,500-row QA backlog. The QA
per-key envelope (4.04s at ~865 keys) puts 200 identifiers on the order of a
one-second apply, 5x under the 15s per-tick bar with headroom for cold I/O.
This run proves the shape that makes that projection safe across backlog
sizes: the apply has no superlinear term in the identifier count, so a
~10,500-row QA backlog drains as short spaced ticks (about fifty windows of
the 200-identifier default) instead of one long transaction. The
200-identifier default (`defaultWinnersChunkMaxIdents`) and the 5,000-row
admission window (`defaultWinnersChunkMaxDirtyRows`, at least three QA
windows) stand.

## Markers

Performance Evidence: the keyed apply scales linearly with the dirty
identifier count (35.941ms at 400, 88.192ms at 1000, 371.061ms at 4000
identifiers, about 90us per identifier with a few ms fixed cost), and the
deleted node stays a stored-table Seq Scan under a Hash Semi Join (338/388/
563+62 buffers at 5,000 stored winners, 1.4/2.1/5.4ms actual). Measured live
at this head on disposable PostgreSQL 18.6 (lane container
`eshu-9805-chunk-pg`), warm cache, steady 5,000-live-row heap, full plans in
[9805-chunked-consume-linearity-plans.md](9805-chunked-consume-linearity-plans.md).
The 200-identifier chunk default holds about a one-second apply under the QA
anchors (5x under the 15s per-tick bar), and a ~10,500-row QA backlog drains
in short spaced ticks; no winners-table index is added and no batch, worker,
lease, or concurrency knob changes.

Observability Evidence: no new signal. Bounded ticks are already counted by
`eshu_dp_supply_chain_impact_winners_chunked_ticks_total` with
`bounded`/`remaining`/`chunk_identifiers`/`chunk_kept_rows` on the resweep
span and the commit log line; the per-phase histogram keeps splitting slow
ticks into reads (`build_idents`, `build_active_facts`) and writes (`apply`,
`commit`). Operators diagnose a slow drain through the chunked-ticks rate
against `dirty_log_depth` and the watermark age, per
[supply-chain-impact-winners.md](../public/reference/telemetry/supply-chain-impact-winners.md).
