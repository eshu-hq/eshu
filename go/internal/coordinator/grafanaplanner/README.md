# Grafana Planner

## Purpose

`grafanaplanner` turns one validated Grafana collector instance into a
deterministic workflow run and one work item per selected target.

## Ownership boundary

The package owns request validation, configuration decoding, target validation
and filtering, trigger selection, requested-scope serialization, deterministic
IDs, and per-target fairness metadata. It performs no network, credential, or
database work.

The root `internal/coordinator` package keeps scheduling order, the reconcile
clock, active and claims gates, collector-egress filtering, tenant-grant
authorization, durable admission, retries, queue and lease behavior, and
telemetry. `internal/collector/grafana` owns provider calls and fact emission
after a worker claims the planned item.

## Exported surface

- `PlanRequest` carries the collector instance, observation time, plan key,
  optional trigger override, and optional scope filter.
- `WorkPlanner` implements the root coordinator's `GrafanaPlanner` interface.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/coordinator/plannercontract` validates safe deterministic plan keys.
- `internal/facts` builds stable generation IDs.
- `internal/scope` supplies the Grafana collector kind.
- `internal/workflow` supplies collector, run, and work-item contracts.

## Telemetry

This pure planner emits no telemetry. The root coordinator records
`eshu_dp_workflow_coordinator_reconcile_total` and
`eshu_dp_workflow_coordinator_reconcile_duration_seconds`; workflow rows,
claim status, and duplicate-admission logs expose durable progress.

## Gotchas / invariants

- Target validation runs before disabled-target and request-scope filtering.
  Blank or duplicate scope IDs are errors even on disabled targets.
- Work items keep configured target order. `RequestedScopeSet` sorts targets by
  scope ID for stable JSON.
- A valid request with no selected targets still returns a populated pending
  run. The parent skips durable admission when the item slice is empty.
- An explicit valid trigger overrides bootstrap. An empty trigger derives
  bootstrap or schedule from the collector instance.
- Scope filters trim whitespace, ignore blanks, collapse duplicates, and match
  configured scope IDs exactly.
- Requested-scope JSON contains target identity only. It omits URLs, credential
  environment names, resource limits, and staleness settings.
- Fairness keys use a nonblank target instance ID as the conflict key and fall
  back to the target scope ID.
- Run, generation, and work-item IDs are deterministic for a fixed instance,
  plan key, trigger, and target scope.

## Related docs

- `docs/internal/design/package-restructure.md`
- `docs/public/reference/source-layout.md`
- `docs/public/observability/telemetry-coverage.md`
