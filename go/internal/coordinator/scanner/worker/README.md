# Scanner-worker scheduler

## Purpose

`scanner/worker` plans configured scanner targets and groups one bounded page
of canonical running-image observations by immutable manifest and authorization
boundary. It does not read source paths, open image layers, or contact another
service.

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
- `DerivedImagePlanRequest` carries one protected discovery page and its
  scanner boundary. `PlanDerivedImageWork` preserves every canonical source
  observation as a binding and emits one work item per boundary-local image.

## Dependencies

The collector scanner-worker package supplies analyzer and target-kind
contracts. `contract` validates plan keys; `facts`, `scope`, and
`workflow` supply stable identities and durable row contracts. This package
does not import its parent. The direct collector enum dependency remains future
extraction debt, so this in-process ownership seam is not independently
extractable.

## Telemetry

None. Planning inherits the parent reconcile metrics, workflow and claim
status, and bounded discovery admission logs.

The parent records only page counts, cursor version and cycle exhaustion; it
does not log registry, repository, runtime reference, or source object IDs.

## Gotchas / invariants

- Run IDs remain a function of instance and plan key. Work-item and generation
  IDs also include analyzer and target scope.
- Duplicate scope IDs fail before any row is returned.
- Derived grouping uses the scan target's namespace, tenant boundary,
  authorization domain, and verified manifest digest. The durable admission
  transaction rechecks each source and its protected manifest verification.
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
