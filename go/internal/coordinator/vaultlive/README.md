# Vault-live scheduler

## Purpose

`vaultlive` plans one workflow work item per configured Vault metadata target
without contacting Vault or resolving credentials.

## Ownership boundary

This package owns `PlanRequest`, configuration validation, and deterministic
workflow-row construction. The parent coordinator retains the planner
interface, scheduling order, plan-key clock, durable admission, retries, and
telemetry. Methods on `coordinator.Service` remain in the parent package.

## Exported surface

- `PlanRequest` carries the collector instance, observation time, and plan key.
- `WorkPlanner` implements the parent's `VaultLivePlanner` interface.

## Dependencies

The collector's `VaultScopeID` helper supplies opaque scope identities;
`plannercontract` validates plan keys; `facts`, `scope`, and `workflow` provide
stable identities and durable row contracts. This package does not import its
parent.

## Telemetry

None. Planning inherits the parent reconcile metrics, workflow and claim
status, and duplicate-admission logs.

No-Observability-Change: this package move adds no metric, span, log field,
queue, worker, lease, or runtime setting.

## Gotchas / invariants

- Stable IDs remain a function of instance, plan key, and opaque target scope.
- Duplicate `(vault_cluster_id, namespace)` targets fail without exposing the
  raw pair in the error.
- Work items preserve configured target order; requested-scope targets sort by
  opaque `scope_id` for deterministic metadata.
- Durable rows omit Vault addresses, token environment names, cluster IDs, and
  namespaces.
- Target keys use an unambiguous separator so colon-bearing components cannot
  collide.

No-Regression Evidence: direct child tests call the production planner and pin
request rejection, deterministic multi-target output, ordering, bootstrap
identity, delimiter safety, duplicate redaction, and connection-material
non-leak. Recursive coordinator and workflow-coordinator tests compile the
root interface, exact service request, and concrete child wiring. Scoped race,
build, and vet checks cover the same boundary without changing the runtime
path.

## Related docs

- `go/internal/coordinator/README.md`
- `docs/internal/design/package-restructure.md`
- `docs/public/reference/source-layout.md`
