# #7166: quiescence-gate probe statistics

## Problem

The canonical-code quiescence gate (`HasUncommittedCanonicalCodeScopes`,
`go/internal/storage/postgres/reducer_graph_drain.go`) probes
`graph_projection_phase_state` once per repository fact with
`(scope_id, generation_id, keyspace, phase, acceptance_unit_id)`. The
planner multiplies the scope and generation selectivities and estimates
rows=1 for the four-column probe while 75 to 150 rows actually match. At
that estimate it prefers migration-156's single-column `generation_idx`
(kept for the retention cascade) or the pkey without pushdown, demoting
`acceptance_unit_id` to a join filter and detoasting
`payload->>'repo_id'` about 9M times per full evaluation.

## Fix

Migration 165 adds a functional-dependency statistics object on
`(scope_id, generation_id)` of `graph_projection_phase_state` plus
`ANALYZE`, following the migration-119 precedent. The gate SQL text is
unchanged. A new `eshu_dp_shared_projection_lane_gate_seconds`
histogram records one probe-latency point per successful gate
consultation (code_calls, repo_dependency), held or open. Code-call
cycles short-circuited by active reducer-graph work return before
the probe and emit nothing, as do failed consultations.

## Measurement

PostgreSQL 18, production DDL for the four gate tables, 619,320 facts /
794 scopes / 119,100 phases / 19,850 generations, `VACUUM ANALYZE`d,
custom plans, three back-to-back runs each (stable within ±2 buffers
across identical reruns; the table shows the repeated value):

| Gate text | Stats | Buffers | Time | Phase probe |
| --- | --- | --- | --- | --- |
| shipped (correlated) | none | 726,673 | 4,871 ms | pkey, no pushdown, rows=1 est vs 75.50 |
| shipped (correlated) | dependencies | 603,253 | 427-455 ms | pkey, 5 quals pushed, rows=1.00 exact |
| shipped (correlated) | ndistinct | 726,673 | 4,871 ms | unchanged (dependencies is the operative kind) |
| B1 anti-join rewrite | none | 9,357,115 | 3,244 ms | generation_idx, join filter over 8.87M rows |
| B1 anti-join rewrite | dependencies | 9,357,115 | 3,218 ms | unchanged (rewrite is stats-immune) |
| B1, generation_idx dropped (txn) | none | 484,168 | 417 ms | pkey, 5 quals pushed (forbidden: breaks #7419) |

An earlier shim carrying an experimental covering index
(`fact_repo_cover_7166_idx`, never committed, since dropped) measured
the rewrite at 0.41 s / 720k buffers against 0.38 s / 483k for the
shipped text; that state does not match production DDL and those
numbers are discarded.

A B1-class rewrite (UNION ALL of two flat anti-joins, fact-driven
branch 1) was implemented, proven answer-identical to a frozen oracle
plus a mid-cycle-commit live test, then reverted: it never beats the
statistics fix in any measured configuration, its plan flips between
parallel and serial across identical executions, and the buffer cost
(13x the shipped text on the vacuumed production-shaped schema) is a
cache-footprint regression. Arbiter verdict (A), independently
re-measured: ship statistics + telemetry, drop the rewrite.

## Transfer caveats (NOT_CHECKED from here)

- The shim holds ~150 phases per generation; migration 156's ops-qa
  census reports ~9 (91,558 rows / 10,167 generations). The shim's
  absolute win (11x) need not transfer; the mechanism (repaired
  rows estimate restoring the pkey pushdown) is distribution-shaped.
- The shim learns scope<->generation dependency 1.0 in both
  directions (one generation per scope in its phase rows). Production
  holds many generations per scope, so the scope->generation
  direction may be weaker there; the generation->scope direction
  holds universally by foreign key, which is the direction that
  repairs this probe's estimate.
- Production effect is judged from the new lane-gate histogram,
  not from this shim.

## Performance Evidence (#7166):

Baseline (shipped gate text, no extended statistics): 726,673
buffers, 4,871 ms, pkey without pushdown, rows=1 estimated vs 75.50
actual. After (same text, `dependencies` statistics on
`(scope_id, generation_id)`): 603,253 buffers, 427-455 ms, pkey with
all 5 quals pushed, rows=1.00 exact. Backend: PostgreSQL 18, local
shim with production DDL for the four gate tables. Input shape:
619,320 facts / 794 scopes / 119,100 phases / 19,850 generations,
`VACUUM ANALYZE`d, custom plans, three back-to-back runs per cell.
Terminal counts: the probe returns one boolean; the phase probe
matches 75-150 rows per evaluation. The change is safe because the
gate SQL text is unchanged (answers cannot change), the statistics
object is additive planner metadata (worst case is a plan no-op),
migration 165 holds only ShareUpdateExclusiveLock and reruns cleanly
(exit 0 with NOTICE skip), and migration 156's `generation_idx` is
kept for the #7419 retention cascade.

## Observability Evidence (#7166):

Four hermetic tests pin the new signal: the code-call lane emits 4
probe-latency points across held+open consultations through the
production `processOnce` path, and the repo-dependency lane pins the
`eshu_dp_shared_projection_lane_gate_seconds` point with
`(domain, reason)` labels; one test per lane pins that a failed
probe emits nothing. `scripts/verify-telemetry-coverage.sh`
passes with the new coverage row. Code-call cycles short-circuited
by active reducer-graph work emit nothing, as do failed
consultations, and the deployable-unit edge path has no instruments
handle and stays dark; all are documented in the coverage row.
Post-merge, the shim-to-production transfer is judged from this
histogram, not from the shim numbers above.
