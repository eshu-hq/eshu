# #11430 keyed-probe shim: the build_active_facts probe cannot shrink 4-5x

Date: 2026-11-25. Lane: drive/11430-drain-bottleneck (executor).
Tree: `f12a82c21e` (clean; no code changed by this lane).
Prior: synthesis named the build_active_facts fact_records probe the winner
(#11430, goal #10671): tick 150-190s to 25-45s via a "shrunken probe".
This shim tests that theory on a disposable database and disproves the 4-5x.

## Claim

At the QA-observed shape (200 identifiers, ~35k scanned copies, ~12k kept,
~10KB payloads) every probe statement in the keyed tick is plan-healthy.
The 104s build_active_facts phase is cold-cache I/O volume (~84k buffers per
tick), not a plan defect. No exact-preserving probe-side code change reaches
4-5x; the measured ceiling across all examined variants is ~1.6x, most give
~1.3x or zero, and each carries migration, contract, or telemetry risk.
Do not build the winner as specified; the decision below belongs to the owner.

## Shim setup (disposable PostgreSQL 18.7, local, since removed)

- `fact_records`: 335,000 rows. Matched set 35,000 rows (200 identifiers x
  ~175 copies across 40 generations, one live generation per scope, 12,000
  live copies) with ~10KB jsonb payloads (matches #9975: 160MB per ~15k
  rows). Distractors: 300,000 small findings.
- `scope_generations`: 2,004,400 rows (2M background + 400 live-shape).
- Exact shipped indexes: migration 291 five-tuple partial index and the
  active-scan partial index. Exact shipped statement text for the narrow
  probe, the active-facts build, the scope expansion, and the chunk path,
  with 200 identifier rows.

## Measurements (EXPLAIN ANALYZE, BUFFERS)

| arm | result |
|---|---|
| narrow probe, 291 index serving | 13-20ms warm, 36k buffers, Index Scan, 200 searches, 35k rows |
| active-facts build, 12k kept | 13-20ms warm, ~48-49k buffers (12k PK probes + toast + temp write) |
| live_gens at 2M scope_generations | 0.4ms, planner flips to PK nested-loop probes (260 searches) |
| scope expansion with a 200k-row generation | 107-198ms for 212k rows, index-driven |
| identifier build + chunk select/insert/index/restrict | every statement under 20ms except the scope_idents build above |
| narrow probe with the 291 index DROPPED | 11.4s, 3.5M buffers (200 lateral seq scans, warm OS cache, 335k rows) |
| narrow probe with a scope_id-carrying 291 variant | planner still picks Index Scan; with indexscan forced off it picks Bitmap Heap Scan (21k buffers, 20k heap blocks). Even a pure-expression select over the variant never goes index-only |

## Why the 4-5x is unreachable probe-side

- Per kept row the phase pays ~4 buffers (heap + ~2 toast + index): 12k kept
  is ~48k buffers for the fetch plus ~36k for the narrow probe, ~84k total.
  At cold EBS-ish penalty (~0.5-2ms per buffer under load) that models to
  ~40-170s per tick (an estimate, not a measurement), consistent with the
  observed 104s phase and 143-190s ticks. The 1000-bound QA trial
  independently shows ~3ms per kept row cold.
- The only large buffer pool is the narrow probe's 35k heap fetches, done
  solely to read `scope_id` (every other narrow column rides in the 291
  index). Making the probe index-only would need an index-only plan, which
  the shim proves the planner never picks for this expression partial index
  (default costing and forced), so extending the index gains zero.
- Skipping dead-copy heap reads (generation pre-filter) is sound:
  `generation_id` is globally unique (PRIMARY KEY in migration 002), and the
  live set could be checked per copy by CPU hash with no I/O. But it shrinks
  the narrow output to kept rows only, which breaks the #11308 scanned-copies
  signal (pre-filter count), needs a contract version bump with differential
  proof, contradicts the #10839 direction, and still saves only ~25% of
  phase buffers (~1.3x), because the final fetch of kept wide rows is
  workload-inherent: the winner body extracts ~15 payload keys per row.
- Narrowing the temp table to extracted keys does not help either: any key
  extraction still detoasts the full ~10KB value per row, so heap and toast
  reads are unchanged.

## Decision needed (owner or coordinator, not this lane)

1. Check the 291 index state on prod/QA from the live slot (`indisvalid`,
   index size and bloat, EXPLAIN of one tick's narrow probe). The
   missing-index arm above (11.4s at one-third prod rows, warm) fully
   explains a ~100s phase at prod scale cold. If the index is invalid or
   unused there, the fix is an ops rebuild, not a code change.
2. If the index is valid and serving, accept that no exact-preserving probe
   change delivers 4-5x. The remaining levers are inflow reduction
   (content-gated dirty marking, the issue's leading candidate), cache/I-O
   pressure relief (ops), or concurrency redesign (arbiter-level: the tick
   holds the single-flight lock throughout). Each needs an owner decision.
3. Re-open rank #1 (supply_chain_impact domain starvation, ~88% of live
   reducer rows dirty at ~3.7/hr completions): rank #3 was chosen partly
   because rank #1 was unattributed, but rank #3's promised gain does not
   survive measurement.

## Not covered

- No QA or prod access exists from this lane; index validity, the current
  kept/scanned ratio, and per-statement cold times on real data are
  NOT_CHECKED and belong to the coordinator's live slot.
- Absolute cold wall times were not reproduced (local OS cache stays warm);
  the argument rests on exact logical buffer counts, which are cache-state
  independent, plus the corroborating QA per-row figures cited above.
- No code was changed and no contract version was bumped; the tree is as
  found plus this file.
