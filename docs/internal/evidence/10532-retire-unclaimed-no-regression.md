# #10532 retire-unclaimed: no-regression evidence

Sanctioned disposition for never-claimed workflow claim-plane rows
(`POST /api/v0/admin/workflow-retire-unclaimed` →
`WorkflowControlStore.RetireUnclaimedWorkItems`). This note is the tracked
record the perf-evidence gate requires.

## No-Regression Evidence:

The change adds no hot-path work:

- The retire runs only on an explicit operator request against a
  deconfigured instance. No coordinator tick, reconciler, planner, claim,
  heartbeat, reap, or retention loop calls it; no background goroutine was
  added. The steady-state per-tick query count is unchanged.
- Each call issues exactly two autocommit statements: the instance-gate
  `SELECT EXISTS` pair (primary-key-adjacent `collector_instances` probe)
  and one bounded `UPDATE ... RETURNING` (LIMIT 100, newest first).
- Planning shape on PostgreSQL 18.3 (`EXPLAIN`, shared dev database, probe
  instance with zero rows): `Limit -> Sort -> Index Scan using
  workflow_work_items_family_queue_depth_idx` with
  `Index Cond: (collector_kind, status = pending)`, cost 2.50, estimated 1
  row. The lock step re-reads the selected ids by primary key in
  `work_item_id COLLATE "C"` order under `SKIP LOCKED`, so a concurrent
  claim never blocks behind the retire and vice versa.
- Live proof on the same backend (isolated disposable databases, full 318
  migration bootstrap per test): 27 retire tests green (9 live + 8 storage
  hermetic + 3 admin-store + 7 admin-handler), including a 4
  claimant + looping retirer race over 12 rows (both sides won rows,
  every row exactly one winner, zero `40P01` across all runs) and a
  held-row `SKIP LOCKED` proof. Suite wall times of 5-13 s per test are
  bootstrap-inclusive; statement-level work is sub-second and runs at
  operator frequency, not tick frequency.
- Baseline: no sanctioned path existed (raw-SQL deletion was refused), so
  there is no prior steady-state cost to regress. The retired rows leave
  the pending set, which strictly shrinks the claim candidate scans for
  the affected instance.

## Observability Evidence:

- `eshu_dp_workflow_unclaimed_retired_total` (by `collector_kind`,
  `collector_instance_id`, value = retired count, incremented once per
  successful call with a nonzero retired count — zero-count calls tick
  nothing), registered in
  `registerClaimPlaneInstruments` and pinned by
  `TestClaimPlaneInstrumentsRegisterAndEmit`, including the assertion that
  no `scope_id` label is emitted.
- One `workflow unclaimed work items retired` INFO log per call carrying
  kind, instance, and count; scope ids ride only the retired rows and the
  admin response.
- Terminal row truth (`failed_terminal` /
  `operator_retired_unclaimed` / `operator_retire` / `terminal = true`) is
  asserted per row in `TestRetireUnclaimedLiveHappyPath`.
