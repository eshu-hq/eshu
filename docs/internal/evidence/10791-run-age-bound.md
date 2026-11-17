# #10791 run-age bound: no-regression evidence

## Claim

Closing the attempt-budget bypasses (B1) and expiring stale open runs (B2)
does not regress live collector throughput or coordinator maintenance: the
retryable path is unchanged in-budget, the visibility cap only binds above
one hour, and expiry touches only non-terminal runs older than the bound.

## No-Regression Evidence:

- Baseline (observed 2026-11-16): NVD head window pinned ~4d, 285+
  failed_terminal items, zero completions/24h; GHSA healthy (98/24h), so the
  wedge is NVD-specific, not a fleet-wide regression.
- After (disposable PostgreSQL 18, local): `TestExpireStaleRunsLiveHappyPath`
  expires 2/2 stale items with class `run_age_bound_exceeded` and leaves a
  fresh run untouched; `TestExpireStaleRunsLiveIdempotent` re-runs to zero;
  `TestExpireStaleRunsLiveClaimRace` proves exactly-once vs a live
  `CompleteClaim` with no deadlock; `TestExpireStaleRunsLiveGuardRelease`
  proves reconcile closes the run and the guard re-admits the target.
  `TestClaimRetryDelayCappedAtOneHour` pins delays ≤1h with the poll floor
  and sub-cap passthrough intact (the 1h cap binds above one hour even when
  the poll interval is larger; visibility can only move earlier, never later);
  `TestFailRetryableRequeuesWithinBudget`
  pins the unchanged in-budget retryable path.
- Backend: PostgreSQL 18-alpine disposable databases (one per proof, full
  323-migration bootstrap), plus read-only live-state reads
  for the wedge shape (terminal backlog, blocking run, claim cycling).
- Input shape: seeded non-terminal runs (25h old) with pending and claimed
  items vs fresh runs; concurrent expire-vs-complete on one claimed row.
- Why safe: expiry rechecks every predicate on the locked row (EvalPlanQual
  recheck), locks items in work_item_id order (no new lock order), finishes
  claim rows in the same statement, and repeated calls are no-ops; no worker
  serialization, no batch-size change. One additive index (migration 300,
  open pending/claimed items by updated_at): heartbeats already rewrite
  updated_at, so the marginal write tax is one extra index entry per
  heartbeat alongside the existing claimable-index entry.
- Plan proof (#10802, closed in-tree): `TestExpireStaleOpenRunsPlanStaysIndexServed`
  seeds a 200k-row retention-scale backlog on disposable PostgreSQL 18 and
  pins the shipped statement's EXPLAIN: the candidate walk serves the
  work-item backlog from the new index with no sequential scan of that
  table. Runs/claims keep small-table sequential scans (non-terminal runs
  are bounded by the #8829 retention window).

## Observability Evidence:

- New signals: `eshu_dp_workflow_coordinator_stale_runs_expired_total` (counter) +
  `eshu_dp_workflow_coordinator_stale_run_expiry_duration_seconds` (histogram) +
  gauges `eshu_dp_workflow_coordinator_stale_runs_expired` /
  `eshu_dp_workflow_coordinator_stale_run_expired_items`, plus a WARN log
  (`expired stale open runs past the max open run age` with run/item
  counts). Unit-proven via `TestStaleRunExpiryObservationRecorded`; emission
  verified through `RecordStaleRunExpiry` in the maintenance pass.
- Existing signals unchanged: reap, reconcile, and run-reconcile counters
  keep their names and semantics; the expiry pass runs before reconcile in
  the same tick.
