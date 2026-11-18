# #10851 wave-load measurement (condition 4)

Lane: `drive/10851-tx-scope`. Host: local PostgreSQL 18.6 (Ubuntu 18.6),
disposable databases via `postgresproof.OpenDisposableDatabase`, 2026-11-18.
Each gate ran against a fresh bootstrap (337 migrations).

## Counter at zero

`eshu_dp_supply_chain_impact_winners_write_conflicts_total` stayed at zero
across the replay: `TestSupplyChainImpactWinnersWriterTxnReplayLive` reads
the counter through a `ManualReader` after a writer transaction (capture
batch plus retract batch over pre-existing dirty rows, held open across a
concurrent collapse-in-tick settle) plus the final drain, and fails on any
nonzero sum. The two wave gates below fail on any error of any kind
(including 40P01, which they name explicitly), and the counter hooks fire
only on classified errors — so zero errors there means the counter stays
flat there too.

## Exact conservation

- `TestSupplyChainImpactWinnersResweepKeyedSettleDeadlockFreeLive`: PASS.
  One settler races four writers over the same hot keys while collapsing
  first; the gate asserts zero 40P01 and `drained version sums ==
  totalMarks`, with the residual log and staging tables empty.
- `TestSupplyChainImpactWinnersLandingMultibatchInterleaveLive`: PASS at
  both strata with no retry anywhere —
  `writers=2 ... marks=1240 log_version_growth=1240 log_depth_growth=136
  distinct_keys=136` and
  `writers=4 ... marks=2480 log_version_growth=2480 log_depth_growth=152
  distinct_keys=152`.
- `TestSupplyChainImpactWinnersWriterTxnReplayLive`: PASS. 20 pre-existing
  versioned log rows plus a 30-row capture batch plus a 10-row retract batch
  (60 marks) drain to `drained == 60` with the log and staging tables empty
  and the conflict counter at zero.

## No-Regression Evidence:

Baseline: direction-C capture path without conflict hooks (post-#10895 tree).
After: same path plus `RecordSupplyChainImpactWinnersWriteConflict` calls on
writer capture error, writer Commit error, and tick settle error paths.
Backend: disposable PostgreSQL 18.6, local host. Input shape: wave-load
replay (20 pre-existing rows + 30-row capture + 10-row retract batches,
multi-writer interleave at 2/4 writers) plus the full winners live suite.
Why safe: the hooks execute only when an error is already in hand (classified
and counted, then the original error propagates unchanged — no retry, no
extra statements on the happy path); the steady-state write/tick path issues
zero additional queries. Counter stayed at zero across every green run, and
exact conservation held everywhere it is asserted above.

## Observability Evidence:

New counter `eshu_dp_supply_chain_impact_winners_write_conflicts_total` with
`site` ∈ {writer_capture, writer_commit, tick} and `failure_class` ∈
{deadlock, serialization_failure}, registered in
`NewInstruments`, covered by `TestSupplyChainImpactWinnersWriteConflictContract`
(closed site set pinned) and `TestSupplyChainImpactWinnersWriteConflictScrapeNames`
(series shape pinned, unknown-site and empty-class drop pinned). An operator
watches it alongside the staging-depth gauge: conflict counts rising with a
climbing staging depth means victims are rolling back faster than ticks
complete. Telemetry coverage gate agrees
(`verify-telemetry-coverage.sh` green).

## Dead-letter answer

There is no dead-letter queue on this path, by design: a contended-but-valid
mark must not be dropped. On a 40P01/40001 the victim transaction rolls
back — the writer commit failure drops the held capture counts
(`supplyChainImpactTx.Commit`), the tick unwinds through its deferred
rollback — the hook counts the victim under `site`/`failure_class`, the
error propagates unchanged, and nothing retries it here. Re-drive is the
next attempt re-admitting the same window: the reducer pass fails and its
work item re-drives the write, and the next maintainer tick re-admits the
same dirty window. There is no retry budget to exhaust. The victim signal an
operator watches is the staging-depth gauge
(`eshu_dp_supply_chain_impact_winners_staging_depth`, unioned into
`DirtyLogStats`) rising with the new conflict counter: captures queue while
no tick completes.
