# #9975 keyed winners tick: per-phase timing

Refs #9975. Part of #9209, follow-up to [9805-winners-keyed-resweep.md](9805-winners-keyed-resweep.md).

## What changed on the hot path

The keyed winners tick recorded only its whole duration in
`eshu_dp_supply_chain_impact_winners_resweep_duration_seconds{path="keyed"}`. On
QA a few ticks ran 30 to 103 s while the p95 was 2.3 s, and the single number
could not say whether the time went to reading `fact_records` or to writing
winners. `ResweepWinnersKeyed` now times each contiguous group of its existing
statements and records one sample per phase in
`eshu_dp_supply_chain_impact_winners_tick_phase_duration_seconds{phase}`.

The phase set is closed and has eight values. Each is a boundary the code already
has, in tick order:

| Phase | Statements it times |
| --- | --- |
| `claim` | transaction begin and the resweep try-lock |
| `consume` | `CREATE TEMP TABLE` for the consumed rows, the dirty-log `DELETE ... RETURNING`, the tally by source |
| `build_idents` | identifier set, its `ANALYZE`, the identifier count, and the cap check (the stored-winners estimate) |
| `build_active_facts` | keyed active-facts temp table (the probe into `fact_records`), the operator facts, its `ANALYZE` |
| `apply` | the winner select and row writes, and the read of their counts |
| `full_rebuild` | the whole-table rebuild a tick ran instead of the last two phases because it went over the cap |
| `stamp` | the watermark stamp of a tick that consumed nothing |
| `commit` | the transaction commit |

A failed phase records the time it spent before the error, which the existing
resweep histogram does not (it records only a committed tick). A tick that lost
the lock records `claim` only.

No SQL, lock, claim, lease, ordering or batching changed. The statements run in
the same order inside the same transaction. The only additions are a
`time.Now()` pair and one histogram record per phase. The apply read moved into a
helper (`applyWinnersKeyed`) with the same body so its timing has a boundary.

## Measurement

No-Regression Evidence: the per-tick cost of the timers is about 1.3 microseconds
with the instruments wired and about 0.26 microseconds without, against ticks that
run for tens of milliseconds to 103 s.

| Item | Value |
| --- | --- |
| Harness | throwaway Go benchmark, not committed: call `timePhase` for the six phases of a keyed apply tick with a no-op body, once with a nil `Instruments` and once with real `telemetry.Instruments` over a manual reader |
| Command | `go test -run '^$' -bench ScratchSixPhases -benchtime=1000000x -count=5 ./internal/storage/postgres/` |
| Toolchain and host | go1.26.10, darwin, 18 CPUs, load1 7.9 at the end of the run |
| Result, nil instruments | 259.1 to 260.0 ns per tick, 0 B/op, 0 allocs/op |
| Result, with instruments | 1262 to 1285 ns per tick, 1009 B/op, 24 allocs/op |
| Input shape | six `timePhase` calls per op, the phases of a keyed apply tick; an idle tick makes four, a full-rebuild fallback five |
| Baseline | NOT_CHECKED. The code before this change had none of these calls, so the figures above are the whole added work. The cost of a live tick against Postgres is not re-measured here; it is dominated by statements and does not change |

The added work is bounded and does not grow with rows, keys or findings. A tick
runs once per maintainer interval per process, so the allocation is negligible.
Concurrency: the timers run on the goroutine that owns the transaction and read no
shared state; the OpenTelemetry histogram record is safe from any goroutine.
Cardinality: one label with eight values, no key, scope, repository or CVE, so the
series count is eight per process.

## Operator signals

Observability Evidence: the histogram and how to read it are on
[the winners telemetry page](../../public/reference/telemetry/supply-chain-impact-winners.md)
and in the reducer-domains row of
[the coverage page](../../public/observability/telemetry-coverage-reducer-domains.md).

- `eshu_dp_supply_chain_impact_winners_tick_phase_duration_seconds{phase}`: at 3 AM,
  a slow `keyed` tick splits into reads (`build_idents`, `build_active_facts`) and
  writes (`apply`, `commit`). The page gives the `histogram_quantile` query.
- The buckets are the existing resweep buckets (0.05 to 300 s), so a phase under
  50 ms reads as the first bucket. That is enough to see the 30 to 103 s ticks of
  #9975, not to rank sub-50 ms phases.
- The histogram does not tie a sample to one tick. Matching a slow tick to its
  phase uses the time of the jump in the bucket counts.
- Proof of the contract: `TestResweepWinnersKeyedRecordsOneSamplePerPhase` (keyed
  apply, idle tick, over-cap fallback, lost lock),
  `TestResweepWinnersKeyedRecordsTheTimeOfAFailedPhase` (a failure in each phase
  and in the commit, with the delayed statement's time recorded and no later
  phase sampled), and `TestResweepWinnersKeyedWithoutInstrumentsRunsUnchanged`.
  A mutant that skips the record on error fails the failed-phase test.

## Not checked

- The cause of the slow QA ticks. This note adds the signal that will say which
  phase they spend time in; the primary-side `EXPLAIN (ANALYZE, BUFFERS)`, the
  fix and the QA re-read are the other acceptance items of #9975.
- Emission against a live Postgres: the proof uses a scripted transaction, so it
  checks phase boundaries and labels, not statement timings on a database.
