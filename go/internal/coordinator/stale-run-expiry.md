# Stale Open Run Expiry

Maintenance pass that terminal-fails pending and claimed work items of
non-terminal runs older than the max open run age (eshu-hq/eshu#10791), so
one stuck-open run cannot hold its targets' open-target guard forever.

## Order

`runActiveMaintenance` calls `runExpireStaleOpenRuns` immediately before
`runWorkflowReconciliation`, so the same pass that expires the rows also
reconciles the runs closed by the expiry and releases their guards. The
planner then re-mints the freed targets on its next tick.

## Contract

- Bound: `MaxOpenRunAge`, default 24h, via
  `ESHU_WORKFLOW_COORDINATOR_MAX_OPEN_RUN_AGE`. Non-positive values fall
  back to the default; the bound only ever tightens the open-target guard,
  never the cursor or the plan.
- Store entry: `ExpireStaleOpenRuns` on the coordinator `Store`, implemented
  by the Postgres workflow control store. It returns the transitioned items;
  an empty result is a safe no-op, so a repeated pass drains residue.
- Locking: items locked in work item id order under `SKIP LOCKED`, predicates
  rechecked on the locked rows, claim rows finished in the same statement. A
  concurrent completion either wins the row first (expiry skips it) or loses
  to it (completion is fenced); exactly one wins, never both.
- Rollback: set the env to a huge value or revert. Expired items stay
  terminal; the planner re-mints the same deterministic scopes, so no window
  is skipped and no fact is duplicated.

## Operator signals

- Counter `eshu_dp_workflow_coordinator_stale_runs_expired_total` (labels: outcome) and
  duration histogram `eshu_dp_workflow_coordinator_stale_run_expiry_duration_seconds`.
- Gauges `eshu_dp_workflow_coordinator_stale_runs_expired` and
  `eshu_dp_workflow_coordinator_stale_run_expired_items` for the most recent pass.
- WARN log `expired stale open runs past the max open run age` with the
  expired run and item counts.
