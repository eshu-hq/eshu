# Gate-3 close ruling (#10020 batch predrain, 2026-11-14)

Received via arbiter-eshu subagent during the #10020 closeout; coordinator
verified every cited observation before recording. Follow-up P3: #10677.

## Decisions

**1. Q1 — Cycle-4 is still drain-shaped, and Gate-3 CLOSES anyway.**

Cycle-4 is not a steady-state cycle: 1.85M rows over 20 generations (~92k rows/gen)
is fat-backlog shape, and `count_rows` at 637s follows the candidate window, not the
fitted batch. Calling it "non-backlog" would be false. But Gate-3 does not need a
non-backlog cycle. Gate-3 tests the batch-predrain safety invariants — normalized lock
hold, small locked recount, zero over-limit under the lock, tallied skips, no
deadlock — and those held on cycle-4 (0.83s/gen vs the 5.5s/gen bar, recount 0.29s,
`rows_over_batch_row_limit=0`, 182 skips all reason-tallied) and on the three prior
cycles, which include worse shapes than steady state will produce. The plateau
(pending 474–510, arrivals ≈ drain) is the expected live-system equilibrium while sync
runs; "pending = 0" is a state this system may never observably hold, so waiting for it
waits forever and proves nothing the four observed cycles did not. The prior ruling
already voided the absolute `count_rows` bar for backlog cycles; its 637s planning cost
is the already-ruled separate future work and is not reopened here.

Close criteria, all required before the close is recorded:
- Cycle-4's numbers are written into `evidence-10020-batch-predrain.md` as the closing
  cycle, labeled drain-shaped, with the normalized bars tabled per cycle 1–4.
- The confirmation query in Decision 2 passes (11:18 event batch = cycle-4).
- The pod log re-grep in Decision 2 is done and its result recorded either way.

**2. Q2 — No defect. The events are present; they are stamped at cycle start.**

Severity: none (P3 nit below). Confidence: high, from code. Evidence:
`PruneSupersededGenerations` captures `now := s.now()` once, before the predrain phases
and before `Begin` (`generation_retention.go:97`), passes that same `now` into
`pruneCandidates` (`generation_retention.go:168`), which inserts one event per candidate
with it (`generation_retention.go:416`, `generation_retention_events.go:144`). `Duration`
starts after the predrain (`generation_retention.go:109`), so cycle-4's wall clock is
654.9s + 174.2s predrain ≈ 829s; 11:32:49Z − 829s ≈ 11:18:5xZ. The 11:18:37 batch of
exactly 20 events **is** cycle-4's batch — the count match is not a coincidence. Events
and deletes commit in one transaction, so a pruned-without-events cycle is impossible on
this code path.

Coordinator confirmations (2026-11-14): (a) full-pod-log re-grep finds 2 completion
lines (11:32:49, 11:41:43); earlier batches' lines were lost to container log rotation
(pod born 09:50:24Z, verbose JSON logs) — `recordResult` logs unconditionally on success
(`runner.go:346`) and same-tx atomicity proves the lines were emitted, so the gap is
retrieval, not behavior. (b) literal sum reconciliation FAILS and stays disclosed:
the 11:18 batch sums 106,311 rows vs `rows_pruned_total` 1,848,925. Mechanism, code-grounded:
selection runs post-predrain, so event counts are post-predrain residuals; `finish()`
merges predrained rows into `RowsPruned` only for pruned gens while predrained rows of
skipped gens (27 `row_limit_chunked` this cycle) merge with the skip and are recorded
nowhere. No missing writes; the accounting gap is the P3 below, not a Q2 defect.

Residual P3 (non-blocking, tracked as #10677): an event's recorded timestamp lags its
commit by up to a full cycle duration, and predrained rows of skipped generations are
recorded nowhere. Fix shapes: stamp events at insert time in `pruneCandidates`; merge
skipped-gen predrained rows into a visible counter. Neither gates Gate-3.

**3. Q3 — KEV 240-id re-measure: scope-local gate, procedure already defined.**

The procedure is the QA read-only measurement recorded in
`9914-os-advisory-upstream-grouping.md:253`: the 2-kind `cve_lookup_v2` statement over
the same 240 real KEV ids, cold-first then warm, measuring candidate rows read, distinct
generations of the known_exploited scope touched, plan shape, and wall time. The
2026-11-04 baseline: 146,928 candidate rows across 614 retained generations (~612
rows/id), warm ~2.4s, cold-first 19.7s with one run hitting the 30s statement cap.

Run it only when the `known_exploited` scope itself is drained — concrete trigger: that
scope's superseded-generation count is at or below the policy floor
(`MinSupersededGenerations` plus the active generation). Global pending is irrelevant.
Pass bars: candidate rows read collapse from ~146,928 toward ~240 ×
retained-generations-per-id; warm ≤ 2.4s; cold-first inside the 30s cap with a printed
`cve_lookup_v2` key-probe plan; same ids cold and warm.

## What this ruling does not do

- Keep Gate-3 open pending global pending = 0.
- Treat cycle-4 as a steady-state cycle in any record.
- Block on the event-timestamp lag or the `count_rows` 637s.
- Accept the KEV re-measure run against a partially drained `known_exploited` scope.
