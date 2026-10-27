# #9257 factless convergence: no-regression note

A completed claim whose result carries no facts skips ingestion entirely,
so its run waited for reducer publication that could never arrive. The fix
counts completed items with nothing committed and nothing queued as
vacuously published (`FactlessCompletedItems`, `go/internal/workflow`,
`go/internal/storage/postgres/run_reconciliation_factless.go`).

## No-Regression Evidence

- Baseline: `reconcileWorkflowRunOnce` ran 3 read queries per run
  reconcile (progress, phase counts, dead-letter counts).
- After: one additional read-only `SELECT` in the same transaction,
  one indexed probe per completed work item of the run.
- Backend: Postgres 18.6 (QA read replica, `SHOW server_version`).
- Input shape: the wedged run held 1 completed item; scheduled runs
  hold single-digit items. Reconcile runs per active run on the
  coordinator tick, not per fact.
- Measured plan (`EXPLAIN`, no `ANALYZE`, on the replica, incident
  run id): `Nested Loop Anti Join` with `Index Only Scan` on both
  `fact_records_scope_generation_idx` and
  `fact_work_items_scope_generation_idx`, total cost 8.23, estimated 1
  row. Both probes use the `(scope_id, generation_id)` equality
  prefix; no sequential scan at any cardinality.
- Why safe: the probe cannot change data (pure `SELECT`); any error
  fails closed (`return ... %w`, no convergence on probe failure); a
  generation that committed any facts is never miscounted, so the only
  behavior change is runs like the incident's converging instead of
  wedging. Fail-closed companion test pins the fact-committed case.

## No-Observability-Change

Existing signals diagnose this path: the completeness detail string
now names the vacuous convergence (`published for all 1 work items
(1 factless with nothing to project)`), the run-status transition out
of `reducer_converging` is visible in the existing coordinator
completeness gauge, and the wedged-run age pattern that surfaced the
incident is unchanged. No new metric was added for this one-case fix.
