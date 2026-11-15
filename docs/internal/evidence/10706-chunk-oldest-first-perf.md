# Performance Evidence: #10706 oldest-first keyed chunk order

## Change

Keyed chunk selection changed from alphabetical five-tuple order to
oldest-first by producing dirty row (`MIN(captured_at)`, tie-break
`MIN(seq)` carried through the consumed temp table, scope arm re-joins
consumed for per-row provenance). Admission window (oldest 5,000 by
seq), 200-identifier chunk bound, 50k over-cap fallback, exact-applied
delete, scope-remainder conversion, single-flight lock, `S`/`H`
stamping, and maintainer cadence are unchanged.

## Performance Evidence:

Added per-tick cost of the age GROUP BY + ORDER BY (EXPLAIN ANALYZE
medians, disposable PostgreSQL 18, rolled-back transactions):
5k-admit worst case +2.73ms; 50k-identifier worst case +116.00ms.

## No-Regression Evidence:

- Baseline (current code, disposable PostgreSQL 18, `EXPLAIN ANALYZE`
  medians of 3 iterations in rolled-back transactions):
  - 5,000 admitted rows / 5,000 distinct identifiers: build-CTAS
    3.491ms + chunk-select 1.050ms = 4.54ms.
  - 50,000 post-expansion identifiers (keyed-path max, one scope
    expansion with 2 scope rows at different captures + 10 fact rows):
    30.722ms + 6.632ms = 37.35ms.
- After (aged shim, same shapes): 6.545ms + 0.728ms = 7.27ms at 5k;
  145.191ms + 8.165ms = 153.36ms at 50k.
- Added cost: +2.73ms (0.018% of the 15s per-tick bar) at 5k;
  +116.00ms (0.77% of the bar) at 50k. Plans are top-N heapsorts
  (201-row LIMIT) at 53-67kB memory, local temp-table buffers only,
  no spill at either scale.
- Same-set guard held at both scales (current == aged identifier
  counts): the reorder never changes the set, only its order.
- Full winners suite on the fixed build: 125 PASS, 0 FAIL
  (`TestSupplyChainImpactWinners|TestResweepWinners|TestWinnersTempTable`).
- Late-commit inversion pair (INV-A lower seq/newest capture, INV-B
  higher seq/3-day-oldest capture) proves `MIN(seq)` alone would leave
  the captured-oldest queued, so `captured_at` is carried; the shipped
  `MIN(captured_at)` picks the oldest row.
- Admission (PK seq scan, LIMIT 5000) and scope expansion are
  byte-identical in both arms: no new dirty-log index and no
  admission-order change needed.

## Observability Evidence:

- No new collectors, spans, or log lines. Per-tick `duration_seconds`
  on the existing `supply chain impact winners resweep committed` log
  line and the `SupplyChainImpactWinnersResweepDuration` histogram
  (path=keyed) already observe the changed stage; the QA re-read
  accumulates exact per-tick durations from those lines.
- No-Observability-Change beyond the above: the fix adds no operator
  surface, so no new dashboard or alert is warranted.

## Why this is safe

The added work is one HashAggregate + sort of at most the 5,000-row
admitted window per tick, bounded by the existing 201-row LIMIT
top-N shape; measured cost stays under 1% of the per-tick budget at
the 50k-identifier worst case and under 0.02% at the nominal 5k case.
The reorder cannot change which rows are deleted (exact-applied
delete of admitted rows is untouched) nor the watermark math
(`GREATEST(previous, S-or-H)` untouched), so graph truth is preserved
— the parity assertion (full recompute equality) passes unchanged.
