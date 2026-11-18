# #10837 direction C: shared staging collapse performance evidence

Arbiter rulings `goal-ops/ruling-10837-direction.md` (CONDITIONAL APPROVE of
direction C, gated on the prove-theory shim) and
`goal-ops/ruling-10837-adjudication.md` (ACCEPT on the decisive same-run
relative arm; the absolute arm moves to a quiet-host landing gate). This note
records the measured evidence the build ran on: the shim re-prove, the
collapse cost cells, and the live gates that pin the contract.

Backend for every number below: disposable PostgreSQL 18.6 (Alpine,
aarch64) on the shared falsifier host `pg-10827-falsifier`, database
created from `template0` per proof, `ApplyBootstrap` over migrations
309-311. The host is NOT quiet (shared), so absolute numbers travel with
the run; the relative arms travel with the build. Wall-clock budget for
the whole proof set: one approach, no clock resets.

## Performance Evidence:

Shim re-prove (`TestSupplyChainImpactWinnersHotkeyStagingShimLive`,
frozen pre-C direct-upsert arm as the contended baseline, 162 shared hot
keys plus 18-key private tails, same host, same run):

- 4-writer stratum: staging plain INSERT p95 0.41ms vs contended upsert
  p95 72.5ms (ratio 0.006, far under the pre-registered 0.80 kill bar).
- Amortized collapse (GROUP BY over the 162x150 = 24,300-row wave,
  p95 6.31ms, spread over the 150 statements of one wave): 0.042ms.
- Effective staged-mark cost 0.45ms, under the 3.41ms envelope with the
  collapse cost included (relative 5/5; the absolute arm stays a
  quiet-host landing gate per the adjudication).
- Worst-partition ceiling (24,300 mostly-distinct keys): GROUP BY SELECT
  floor 43.6ms on the shim host.

Collapse on the shipped statements
(`TestSupplyChainImpactWinnersStagingCollapseWorstPartitionCostLive`,
24,300 distinct staged keys, one wave, default 50k-row budget): full
merge-upsert plus seq-bounded delete in 0.35s, exact conservation
(24,300 groups, version sums 24,300, empty queue). Staging heap 4.0 MB
before and after; dead tuples 0 at read time (stats lag aside, the
per-tick delete is autovacuum-eligible like the log drain).

Stratified writer gate
(`TestSupplyChainImpactWinnersDirtyWriterStratifiedEnvelopeLive`,
production staging INSERT, private 60-key sets): 1-writer control
p95 1.37ms; 2-writer p95 2.17ms (ratio 1.59, backstop 3.0); 4-writer
p95 3.21ms (ratio 2.35, backstop 3.5); reference arm (pre-C upsert
3.41/3.89ms) kept as an upper bound. Green at every writer count.

No-regression basis for landing: the adjudication's distinct-key
total-cost proof and the quiet-host absolute re-prove stay open landing
gates (tracked, not waived); this note is the pre-build and build
evidence only.

## Observability Evidence:

New signals, all registered in
`registerSupplyChainImpactWinnersKeyedInstruments` and recorded on the
tick path (nil-receiver safe, no key/scope/CVE labels):

- `eshu_dp_supply_chain_impact_winners_tick_phase_duration_seconds{phase="collapse"}`:
  the in-tick collapse wall time, sampled every tick that holds the
  resweep lock (including empty-queue ticks). A slow collapse reads as a
  slow staging drain; phase tests pin one sample per ran phase.
- `eshu_dp_supply_chain_impact_winners_staging_depth` and
  `..._staging_oldest_age_seconds`: the staging backlog at the last tick
  this process sampled (setter: `RecordSupplyChainImpactWinnersStaging`,
  fed by the unioned `DirtyLogStats` every tick before the lease claim).
- `eshu_dp_supply_chain_impact_winners_staging_collapsed_rows_total`:
  rows collapsed per tick (the seq-bounded delete count).

At 3 AM: a slow tick splits by phase; `collapse` is the staging drain.
A climbing `staging_depth` with a flat log depth means the capture rate
outpaces the 50k-row per-tick collapse budget. `dirty_log_depth` and
`dirty_log_oldest_age_seconds` union both queues, so they stay the
single backlog gauges; the staging gauges separate the queue behind it.
