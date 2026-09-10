# Coordinator Planners

## Purpose

`internal/coordinator/planner` groups the coordinator's workflow-planning
implementations and their shared plan-key contract under one readable path.

## Ownership boundary

This directory is a namespace, not a runtime layer. Leaf packages turn bounded
requests into deterministic workflow rows. The parent `internal/coordinator`
package continues to own scheduling order, durable admission, retries, queue
and lease behavior, and telemetry.

## Exported surface

None. Import the leaf package that owns the required planner or
`planner/contract` for shared plan-key validation.

See `doc.go` for the package contract.

## Dependencies

None. The namespace package contains documentation only.

## Telemetry

None. Leaf planners emit no telemetry; coordinator runtime operations retain
their existing signals.

## Gotchas / invariants

- Do not put orchestration or shared mutable state in this namespace package.
- A leaf must not import the parent coordinator package.
- Keep request types with their leaf planners unless a separately reviewed
  ownership change establishes a neutral contract.

## Related docs

- `go/internal/coordinator/README.md`
- `docs/internal/design/naming-remediation.md`
