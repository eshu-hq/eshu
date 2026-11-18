# #10863 — stale-generation cleanup out of the reducer claim

## What changed

Both reducer claim statements (`claimReducerWorkQuery`,
`claimReducerWorkBatchQuery`) and the batch funnel census no longer run
the whole-queue supersede sweep. They compose
`reducerClaimStaleGateCTEs()` (`reducer_claim_stale_gate_sql.go`): a
read-only, once-per-statement stale set that fences the same rows at
hand-out, rechecked on the locked rows in the batch claim's two lock
arms. Terminalization moved to the standalone bounded sweep
`supersedeStaleReducerGenerationsQuery`
(`ReducerQueue.SupersedeStaleReducerGenerations`), driven periodically
by `ReducerStaleGenerationSweeper` from `cmd/reducer` (30 s interval,
500-row statements, at most 10 passes per cycle, `SKIP LOCKED` so every
replica may sweep). The sweep reuses the shipped predicate, Crossplane
carrier arm, and terminal UPDATE verbatim through
`supersedeInactiveReducerGenerationsCTEs(lockTail)`; the readiness hold
(#4445/#7664), fan-out-debt spare (#10060, now the shared
`supplyChainFanOutDebtSpareSQL`), Crossplane carrier rule
(#10106/#10269/#10325), EvalPlanQual lock rechecks, container-image
epoch stamps, and the search-doc reserved slot are unchanged. A stale
row that somehow leased is still refused by the runtime generation
guard before its handler runs (`Runtime.execute`), except the #10060
fan-out-debt execution the guard deliberately allows.

## Performance Evidence: before/after on the replay shape

Method (identical for every number below): disposable Docker Postgres
18-alpine on loopback, rolled-back `EXPLAIN (ANALYZE, BUFFERS)` of the
full batch claim text, 2 warmups + 7 reps interleaved per variant,
median execution ms and top-node buffers (hit+read+dirtied+written).
Seed: the #10862 replay mix (`seedReducerQueue10862Replay`, 8,713
waiting rows) widened to a QA shape — 4,000 `ingestion_scopes`, 7,400
`scope_generations`, 43,713 `fact_work_items` of which 35,000 terminal
(30,000 succeeded, 4,000 failed, 1,000 dead_letter). Stale phase adds
200 stale pending rows on older generations plus 6,000 stale
failed/dead_letter rows.

Step-0 gate shim (throwaway test over string-derived variants of the
shipped claim text, run 2026-11-18):

| Variant | Clean ms / buffers | Stale-200 ms / buffers |
| --- | --- | --- |
| Shipped (in-claim sweep) | 83.289 / 85,476 | 275.449 / 299,980 |
| Naked (no sweep, no fence) | 48.043 / 3,892 | 49.817 / 4,594 |
| Gate A (this change) | 67.033 / 29,684 | 69.637 / 31,447 |
| Gate A + lock rechecks | 67.031 / 29,696 | 70.025 / 31,508 |
| Funnel read-only sweep as gate | 82.913 / 85,518 | 110.025 / 121,792 |

Gate A versus shipped: −65.3% buffers clean, −89.5% stale; median time
below shipped in both phases. With `$5` (projector-drain) true on the
stale shape: shipped 274.219 ms / 331,822 buffers, gate 69.724 ms /
32,194. Candidate sets: clean EXCEPT 0/0 with identical order; stale
EXCEPT 0/0 with zero stale rows in either candidate set; full-claim
hand-out (16 rows) identical, while the naked variant handed out 8 of
16 stale rows — the representative-starvation/fencing failure the gate
exists to prevent. The funnel-style read-only sweep replay measured no
better than the shipped sweep, so it was rejected as the gate shape.

Built-path after-measurement (`TestReducerQueue10863BatchClaimReplay`,
unmodified 8,713-row replay seed, same container): drained 8,713 in
23.1 s, `ClaimBatch` p50 25.963 ms / p95 43.934 ms, explain median
50.149 ms / 4,847 buffers at full depth. Shipped build on the identical
seed before this change: `ClaimBatch` median 50.638 ms / p95 52.610 ms,
12,941 buffers (prior perf-verify run). Single-claim replay
(`TestReducerQueue10862ReplayShapeAndDrain`) after: p50 0.006 s /
p95 0.008 s, flatness 0.806. No QA percentage is claimed: QA is
buffer-bound at a different scale, and these numbers prove direction
and mechanism on the replay shape only.

## Correctness evidence

- Acceptance 3 (same hand-out when nothing is stale): shim EXCEPT 0/0
  with identical order (clean phase above);
  `TestClaimBatchGatePrecomputeStaleHeavyExplain` and the rank-once /
  gate-precompute differentials stay green.
- Acceptance 4 (stale refused before cleanup runs):
  `TestReducerClaimStaleGateRefusesStaleBeforeSweepLive` (both claim
  paths refuse, row stays pending, sweep then terminalizes exactly it),
  `TestReducerClaimStaleGateSparesLive` (fan-out debt and sole
  Crossplane stay claimable; carrier-held stale Crossplane fenced).
- Acceptance 5 (concurrent claims + expired hold on built code):
  `TestReducerClaimStaleGateStormExpiredHoldLive` (8 claimers, 32 fresh
  + 8 stale + 1 expired hold: fresh exactly once, hold reclaimed once,
  stale never delivered),
  `TestReducerStaleSweepBoundedConcurrentLive` (4 sweepers, limit 3,
  exactly-once total), plus the retargeted #8205 lock-order family
  (`TestReducerClaimSweep*Live`: 150 rounds, 0 × 40P01).
- The #10269 coalesce matrix, #4445/#7664 readiness-hold proofs, #10060
  fan-out-debt cases, and #7320 prior-failure fold proofs were updated
  to drive claim + sweep in production order and stay green; the sweep
  statement keeps the locked-set carrier semantics and the fold.

## Observability Evidence: sweep signals

New instruments (registered in `instruments_reducer_stale_sweep.go`):
`eshu_dp_reducer_stale_generation_swept_rows_total`,
`eshu_dp_reducer_stale_generation_sweep_passes_total{outcome}`,
`eshu_dp_reducer_stale_generation_sweep_duration_seconds{outcome}`
(outcome ∈ {ok, failed}, the existing outcome dimension; no scope or
work-item labels). Each cycle that supersedes rows logs
`reducer stale-generation sweep` with `rows_superseded`, `passes`,
`duration_seconds`; failures log with
`failure_class=reducer_stale_sweep_error`. At 3 AM: flat swept-rows
with growing fenced stale backlog in queue status means the sweeper is
stalled (claims stay correct via the gate); rising sweep duration with
full batches means stale backlog is outrunning the 500×10 per-cycle
bound.
