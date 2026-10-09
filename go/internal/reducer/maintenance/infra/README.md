# Infra Inventory Reconcile Runner

## Purpose

Keeps the infra read model (#6793) equal to `content_entities` when
some content writer did not derive it: an older binary during a rolling
upgrade, a manual SQL change, or a restore. Moved here from the flat
`maintenance` package under #7648.

## Ownership boundary

Owns the reconcile loop, the suspect/repair two-cycle rule, and the
fence accounting. Does not own the reconcile page SQL or the persisted
walk cursor (the storage `Reconciler` implementation), the infra read
model itself, or the `Service` side-runner startup loop in the reducer
root.

## Exported surface

- `Runner`, `Config` — the reconcile loop and its bounds
- `Request` — one cycle's input (budget, suspects, persisted page)
- `Batch` — one cycle's result (repos, cursor, wrap, fence marks)
- `Repo` — one repository's check outcome
- `Reconciler` — storage port (`ReconcileInfraInventory`)

See `doc.go` for the full contract.

## Dependencies

- `internal/telemetry` — counters, gauges, span, log attributes
- `pkg/log` — structured error logging

Never `internal/reducer`.

## Telemetry

- Metrics: `eshu_dp_infra_inventory_reconcile_total{outcome}`
  (`match`/`suspect`/`repaired`/`fenced`/`error`),
  `eshu_dp_infra_inventory_reconcile_duration_seconds`,
  `eshu_dp_infra_inventory_dirty_repos`,
  `eshu_dp_infra_inventory_dirty_oldest_age_seconds`
- Spans: `reducer.infra_inventory_reconcile` (repos checked/suspect/
  repaired/fenced/failed, walk wrapped, fence marks)
- Logs: `infra inventory reconcile cycle completed`; per-repo
  WARN/ERROR lines with `event_name`
  (`infra_inventory.reconcile.drift`/`.fenced`/`.failed`) and
  cycle-failure ERROR (`infra_inventory.reconcile.cycle_failed`)

## Gotchas / invariants

- Repair requires two consecutive differing cycles; never repair or
  report drift on a single check.
- The fence gauges record every cycle, ready or not, so the reason
  unscoped reads stay on the graph is visible.
- The unexported `contextDone` helper duplicates
  `retention.contextDone`; the two shared one copy before the #7648
  split.

## Related docs

- `docs/public/observability/telemetry-coverage.md`
