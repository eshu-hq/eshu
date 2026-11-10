# #9975: keyed winners probe index and per-identifier probe

Issue #9975 (parent #9805) tracks the keyed winners tick breaching its bars
(keyed p95 at most 15 s, peak watermark age at most 60 s) after migration 279
fixed the dead-version half. This note records the two shims that selected the
fix, what the fix is, and what remains unproven until it rolls.

## Classification

- Candidate volume per tick, not dead tuples: the probe fetched every active
  candidate of every touched package triple as wide `fact.*` rows and filtered
  c0/advisory after the heap fetch. After migration 279 the probe reads 1.147
  buffers per row, so the remaining cost is the number of candidate rows and
  their payload bytes (about 10 KB each on QA), not dead versions.
- Not lock or consume time: per-phase timers put the current pod's tick time at
  `build_active_facts` 68% (p95 17.1 s) and `apply` 31%; claim, consume,
  build_idents and commit are immaterial.

## Shim 1: capture attribution (rejected as the fix)

Prometheus, QA, 2026-11-10 (`capture_rows_total` daily increase by source):
`fact_upsert` is 92–99% of captures every day. 2026-11-09 spiked to 2,030,550
`fact_upsert` rows (keys_sum 1,786,920, keyed p95 56.7 s) against 83,714–276,328
on 2026-11-06..08. But on 2026-11-10 captures were back near baseline (69,780
`fact_upsert` by 10:50Z) while the current pod still failed both bars (p95
18.9 s), so capture volume alone does not explain the failure and
write-on-change at the writer is not the selected fix. The writer already
skips the capture of a same-generation rewrite whose content digest is
unchanged (`dirtyIdentifiersToCapture`); a new generation mints new fact ids,
so its rows are captured by design. The 2026-11-09 surge cause stays open.

## Shim 2: fetched vs kept, and the no-index rewrite (insufficient)

Read-only `EXPLAIN (ANALYZE, BUFFERS)` on the QA primary, 2026-11-10,
statement timeout set, over a sparse tick shape (200 identifiers, one per
triple, drawn from stored winners):

| Shape | Exec | Fetched | Kept | Kept payload |
|---|---|---|---|---|
| Shipped per-triple probe | 3.20 s / 2.94 s | 15,888 rows, 160.6 MB | 2,720 rows | 27.3 MB |
| Ident filter inside the lateral, no new index | 1.84 s / 1.96 s | same heap fetches | 2,720 rows | 27.3 MB |

Fetched:kept is 5.84x. Row sets are identical (count, payload bytes and a
fact-id hash sum all match). The no-index rewrite cuts about 37%, under the
50% bar for shipping it alone: the heap still fetches every candidate to
evaluate the cve/advisory payload expressions, because no index carries them.
That made a new index admissible under the ruling.

## Shim 3: index theory on a disposable database

The same probe shapes on a throwaway local PostgreSQL 18 (43,000 synthetic
findings with about 8 KB payloads, one hot triple of 3,000 candidates, a
25-identifier sparse tick keeping 5 of them):

| Shape | Exec | Index rows returned |
|---|---|---|
| Shipped per-triple probe | 48.5 ms | 3,200 candidates |
| Per-identifier probe, no new index | 30.9 ms | 25 kept, 3,893 buffers |
| Per-identifier probe + new index | 0.45–0.57 ms | 25 kept, about 152 buffers |

Row sets identical. Synthetic and small: it proves the mechanism (index
conditions on all five expressions, heap only for kept rows), not QA timings.
QA timings come from the phase histograms after the carrying roll.

## Fix

- Migration 291 creates `fact_records_supply_chain_impact_winners_keyed_idx`
  concurrently: package triple, coalesced c0, coalesced advisory_id, fact_id,
  generation_id, partial on non-tombstoned findings. The bootstrap fragment is
  in `schema_fact_records_supply_chain_impact_indexes.go`. Cost: one more
  partial index over the finding rows only (about 1.13M rows, an estimated
  0.6 GB on QA against 98 GB of existing `fact_records` indexes), maintained by
  finding writes and cleaned by the same autovacuum cadence migration 279 set.
  The old package-lookup index stays: other triple probes use it, and dropping
  it is a separate, measured decision.
- `buildWinnersKeyedActiveFactsSQL` probes once per identifier with all five
  equalities inside the lateral, still inside a MATERIALIZED CTE, still joined
  to the active scope/generation afterwards. The identifier table holds
  distinct five-tuples and a fact carries one c0/advisory pair, so the probe
  returns each kept fact once. Winner selection, apply, the single-flight
  lock, the lease and the audit backstop are untouched; this cuts work per
  tick, it does not serialize anything.

## Performance Evidence:

QA before (2026-11-10, current pod 07:20–10:38Z, vacuum-free): keyed p95
18.86 s, `build_active_facts` p95 17.14 s, post-startup watermark peak 147.9 s.
Shim 2 above is the same-ident-set before/after for the probe statement on the
QA primary without the index; with the index the heap fetches fall by the
fetched:kept ratio (5.84x on the sparse shape, more when fewer keys of a hot
triple are dirty). Acceptance is the ruling's: after the carrying roll, a QA
re-read within 1 h and a second read over a high-volume window must show keyed
p95 at most 15 s (target 10 s), peak watermark at most 60 s read as the maximum
over the window of the per-instant `min()` across instances, and no keyed tick
over 30 s in the second window. Until that re-read, QA impact is NOT_CHECKED;
#9805's 5x clause also stays unproven.

## Observability Evidence:

No new signal: the existing `tick_phase_duration_seconds{phase="build_active_facts"}`
histogram is the before/after readout, and the row-set parity suites plus the
plan-shape test pin correctness. The telemetry page records the probe cost
model and the `min()` peak rule operators verify against.

## Tests

- `TestSupplyChainImpactWinnersResweepKeyedProbePlanLive` (updated): the
  analyzed plan uses the new index once per identifier (50 searches), returns
  only the 50 kept facts over a 3,000-candidate hot triple, keeps the
  MATERIALIZED CTE fence, and never seq-scans `fact_records`, under both
  scope-table statistics states. It failed on the shipped probe before this
  change.
- `TestSupplyChainImpactWinnersResweepKeyedMatchesRecomputeAcrossChangeClassesLive`
  (existing parity differential): keyed vs full recompute across hot-triple
  rewrites, tombstone flips, generation flips, operator overrides and
  advisory-only keys stays green on the new probe.
