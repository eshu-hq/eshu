# Scanner-worker scheduler

## Purpose

`scannerworker` plans one workflow work item per configured scanner target. It
does not read source paths, open image layers, or contact another service.

## Ownership boundary

This package owns `PlanRequest`, configuration decoding and validation,
deterministic workflow-row construction, requested-scope privacy, configured
target order, and fairness-key construction. The parent coordinator keeps the
planner interface, scheduling order, plan-key clock, active and claims gates,
tenant-grant and collector-egress gates, durable open-target admission, retries,
queue and lease behavior, and telemetry. Methods on `coordinator.Service`
remain in the parent package.

## Exported surface

- `PlanRequest` carries the collector instance, observation time, and plan key.
- `WorkPlanner` implements the parent's `ScannerWorkerPlanner` interface.

## Dependencies

The collector scanner-worker package supplies analyzer and target-kind
contracts. `plannercontract` validates plan keys; `facts`, `scope`, and
`workflow` supply stable identities and durable row contracts. This package
does not import its parent.

## Telemetry

None. Planning inherits the parent reconcile metrics, workflow and claim
status, and duplicate-admission logs.

No-Observability-Change: this package move adds no metric, span, log field,
queue, worker, lease, or runtime setting. Parent scheduling and durable
admission remain covered by `eshu_dp_workflow_coordinator_reconcile_total` and
`eshu_dp_workflow_coordinator_reconcile_duration_seconds`.

## Gotchas / invariants

- Run IDs remain a function of instance and plan key. Work-item and generation
  IDs also include analyzer and target scope.
- Duplicate scope IDs fail before any row is returned.
- `requested_scope_set` excludes runtime-local roots and artifact locators.
- Work items retain configured target order, while requested-scope targets are
  sorted by scope for stable metadata.
- Fairness remains partitioned by collector instance and target kind.
- The parent keeps the durable open-target tuple of collector kind, collector
  instance, scope, and acceptance unit. This package performs no I/O and owns
  no goroutine, lock, transaction, retry, claim, or lease.

No-Regression Evidence: focused child tests call the production planner and
check deterministic IDs, requested-scope privacy, duplicate rejection, and
required target paths. Parent coordinator and workflow-coordinator command
tests compile the root interface, service call, and concrete child wiring.

## Related docs

- `go/internal/coordinator/README.md`
- `docs/internal/design/package-restructure.md`
- `docs/public/reference/source-layout.md`
