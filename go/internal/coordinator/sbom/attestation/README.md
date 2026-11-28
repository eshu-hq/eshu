# SBOM and attestation scheduler

## Purpose

`sbom/attestation` plans one workflow work item per configured hosted SBOM or
attestation target without reading the document or contacting its provider.

## Ownership boundary

This package owns `PlanRequest`, configuration validation, and deterministic
workflow-row construction. The parent coordinator retains the planner
interface, scheduling order, plan-key clock, durable admission, retries, and
telemetry. Methods on `coordinator.Service` remain in the parent package.

## Exported surface

- `PlanRequest` carries the collector instance, observation time, and plan key.
- `WorkPlanner` implements the parent's `SBOMAttestationPlanner` interface.

## Dependencies

`contract` validates plan keys; `facts`, `scope`, and `workflow` provide
stable identities and durable row contracts. This package does not import its
parent.

## Telemetry

None. Planning inherits the parent reconcile metrics, workflow and claim
status, and duplicate-admission logs.

## Gotchas / invariants

- Stable IDs remain a function of instance, plan key, and target scope.
- Duplicate scope IDs fail before any row is returned.
- Durable requested-scope metadata excludes document URLs and credentials.
- Fairness remains partitioned by collector instance and artifact kind.

## Related docs

- `go/internal/coordinator/README.md`
- `go/internal/coordinator/sbom/README.md`
- `docs/internal/design/package-restructure.md`
- `docs/public/reference/source-layout.md`
