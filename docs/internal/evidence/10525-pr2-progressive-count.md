# #10525 PR-2: Progressive Counting-Window Performance Evidence

PR-2 replaces the full-set planning count over the oldest-100 superseded
generations (83 scopes) with a progressive window-6 oldest-first prefix:
the maximal prefix whose cumulative own-facts stay within 100000. The
planning count is a batch-choosing heuristic only; the unchanged recheck
recount (final window) and the mandatory re-lock recount stay
authoritative. Whole-prefix recounts neutralize attribution drift. The
fail-closed key-index guard and the per-access 294 guard are unchanged.
No SQL, index, lock, or knob changes.

Measured on the PG 18.3 QA primary, read-only, `work_mem=64MB`,
`force_generic_plan` (coordinator-verified QA probe).

## Performance Evidence:

Baseline (full oldest-100 superseded set, 83 scopes): full-100 warm plain
`EXECUTE` 48.6s wall; full-100 `EXPLAIN ANALYZE` 152.3s.

After (window-6 prefix): per-generation own-facts 5984 / 5716 / 4844 /
1157 / 5086 / 1148, cumulative 23935; rank 7 own = 163304 blows past the
100000 cap, so the window stops at 6. Window-6 count 883.552ms warm plus
selected-only recount 1152.971ms warm = 2036.5ms exec (walls
893.8 + 1360.5 = 2254.3ms). That is 2.95x under the ~<=6s bar and 14.98x
under the 30.5s planning budget.

Selected rows 28075 of 1360890. Generation-owned arms 204/204 exact vs
full-100; content arms 0 exceedances.

Provenance note: the PR problem statement cites a 34.6s warm full-batch run of the verbatim Gate-3 full-batch query (oldest 100 superseded generations, 83 scopes) with work_mem=64MB and force_generic_plan as warm plain EXECUTE, measured 2026-11-12 on the same PG 18.3 QA primary after migration 294 (ledger 2026-11-12 13:59:36Z); it differs from the 48.6s probe wall above only by live-QA load at measurement time, and both exceed every bar, so no decision changes.

Why safe: the planning count only chooses the batch; correctness rests on
the two unchanged recounts (recheck of the final window, re-lock before
admission). Arbiter Verdict A admits the theory-gate bars
(~<=6s execution, 30.5s planning budget) and both are cleared with margin.

## No-Observability-Change:

No new metric, signal, phase label, or status. The existing `count_rows`
phase is reused (summed, truthful); `ScopeLockHold` semantics are
unchanged. Hence no observability change.
