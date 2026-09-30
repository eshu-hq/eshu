# #7389 review rounds: findings F1-F7 and their closing proof

Companion to [7389-superseded-writer-overlay.md](7389-superseded-writer-overlay.md),
which holds the problem, rulings, measurements and verification.

## Review round (#7389 F1-F7)

F1, graph_dirty was dropped as an unchanged snapshot. The collector took a
full snapshot for a dirty scope but nothing marked it a reconciliation, so the
fact builder kept the content-derived freshness hint and the ingestion store's
unchanged-generation skip dropped it (`refresh_skipped=true`) whenever the
tree matched the active generation's. The reproduction's hint
(`commit:generation_id`) hid it. Fix: `graph_dirty` is a reconcile reason.
`reconcilePolicy.decide` and the new `decideGraphDirty` share one
`throttle` (a pending full younger than Interval is `reconcile_in_flight`, a
failed or superseded-before-activation full younger than Interval/4 is
`reconcile_retry_backoff`); a fresh full never holds graph_dirty off, because
a full that activated before the writer wrote does not clean the graph.
`decideForScope` probes for writers only when the sweep is not due and not
throttled, inside the per-cycle budget check, so it is one probe per scope per
cycle. A forced graph_dirty reconcile takes the existing forced path
(`Reconcile` flag, empty hint via `GenerationFreshnessHint`), counts against
`ESHU_REPO_RECONCILE_MAX_PER_CYCLE` (a budget-exhausted cycle leaves the scope
on its delta, a no-op at the active head, and it is forced one cycle later),
and emits `eshu_dp_collector_reconciliation_full_snapshots_total{reason=
"graph_dirty"}` and `git_reconcile_forced`. The `graph_dirty` label on
`eshu_dp_collector_delta_baseline_fallback_total` is dropped: a forced
reconcile never counts a fallback, so the reconciliation reason is the one
signal. The redundant branch in `syncExistingRepository` is removed. A writer
lookup error keeps the scope on its normal path and is decided again next
cycle. Reconciliation disabled (`ESHU_REPO_RECONCILE_INTERVAL_HOURS=0`) also
disables the graph_dirty heal.

```text
RED (d3609fdb6 collector semantics: graph_dirty full carries no Reconcile flag),
content-derived hint in the Neo4j harness:
  refresh_skipped=true ... generation_id="gen-7389-full-c"
  superseded_delta_overlay_live_test.go: generation gen-7389-full-c status = "", want "active"
  --- FAIL: TestDeltaRefusedAtAckLeavesOverlayLive
RED (collector, d3609fdb6): TestGraphDirtyForcesReconcileAtActiveHead (forced = false),
  TestGraphDirtyRespectsReconcileThrottle, TestGraphDirtyCountsAgainstTheReconcileBudget
GREEN: the heal full's hint comes from ReconcileSweepDecision on the production
IngestionStore (reason=graph_dirty) through GenerationFreshnessHint; all pass.
```

F2, narrowed claim: the heartbeat never abandons a started projection write
to a newer generation; every other retirement of a started write is healed by
a forced full. The claim path's stale-generation supersede and Ack's obsolete
supersede still retire a marked pending generation whose work is retrying
(rollback signal above).

F3, heartbeat vs refusal deadlock. The first heartbeat locked
generation-then-work; `markProjectorDeltaBaselineRefusedQuery` locks
work-then-generation and takes no scope row. A heartbeat 40P01 cancels the
whole projector worker pool. The heartbeat now locks scope, work (full lease
fence), generation, all SKIP LOCKED.
`TestProjectorHeartbeatNeverDeadlocksWithBaselineRefusal` pauses the refusal
with a sleep trigger after it takes the work row and runs the heartbeat in
that window:

```text
RED (d3609fdb6 heartbeat): refusal = mark delta baseline refusal: ERROR: deadlock detected (SQLSTATE 40P01)
GREEN: heartbeat no-op (SKIP LOCKED), refusal marks the work row; interleave x1000 both=0, deadlocks delta 0
```

Supersede benchmark on the same 1,002,001-row seed and targets (501 rounds,
generic plan, BEGIN/ROLLBACK, fresh container, same host, run back to back):

```text
case   stmt      median_ms     min_ms     p90_ms  med_buf   rows
T1hot  sup_cur      0.0840     0.0740     0.0960       27      0
T1hot  sup_lck      0.0930     0.0820     0.1040       27      0
T1typ  sup_cur      0.0770     0.0650     0.0880       24      0
T1typ  sup_lck      0.0850     0.0740     0.0960       24      0
T2     sup_cur      0.1700     0.1460     0.1970       70      1
T2     sup_lck      0.2000     0.1720     0.2280       81      1
T3     sup_cur      0.1740     0.1460     0.2090       70      1
T3     sup_lck      0.0770     0.0680     0.0890       19      0
T4     sup_cur      0.1590     0.1360     0.1780       64      1
T4     sup_lck      0.1860     0.1560     0.2140       71      1

T1hot  sup_cur      0.0810     0.0700     0.0920       27      0
T1hot  sup_lck2     0.1040     0.0910     0.1180       33      0
T1typ  sup_cur      0.0730     0.0630     0.0870       24      0
T1typ  sup_lck2     0.0980     0.0860     0.1110       30      0
T2     sup_cur      0.1640     0.1470     0.1890       70      1
T2     sup_lck2     0.2170     0.1900     0.2430       87      1
T3     sup_cur      0.1710     0.1420     0.2020       70      1
T3     sup_lck2     0.0910     0.0790     0.1030       26      0
T4     sup_cur      0.1630     0.1340     0.1870       66      1
T4     sup_lck2     0.1920     0.1680     0.2140       75      1
client-wall p50, T1hot: cur 0.186 ms, sup_lck2 0.213 ms; T1typ: cur 0.154 ms, sup_lck2 0.180 ms
```

`sup_cur` is the pre-#7389 statement, `sup_lck` the first shipped
(generation-then-work) shape, `sup_lck2` the shipped work-then-generation shape.
Absolute median execution delta of `sup_lck2` against `sup_cur`: T1hot +0.023
ms, T1typ +0.025 ms, T2 +0.053 ms, T4 +0.029 ms (T3 -0.080 ms: it no longer
supersedes). Against the waived `sup_lck`: +0.011 to +0.017 ms, from locking
the work row on every heartbeat (6 more buffers). The waiver's +10 to +35 us
becomes +23 to +53 us (re-affirmed by the round-2 arbiter ruling) per heartbeat (about every 20 s) per running work item,
the steady-state heartbeat at +23 to +25 us. A first run without the shim's
target setup (all rows 0, 6 buffers) was discarded as invalid.

F6: `projector write marker waiting for busy generation row` WARN on the first
marker deferral and every 30th, with scope, generation, attempt and retry
count (`TestServiceWriteMarkerDeferralIsLogged`, RED with the logger disabled).
