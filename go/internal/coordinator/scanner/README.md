# Coordinator scanner contracts

## Purpose

`coordinator/scanner` groups coordinator-side contracts used to plan scanner
work. It is a documentation-only namespace; implementation belongs in its leaf
packages and runtime behavior stays in the coordinator root.

## Ownership boundary

The namespace owns no runtime declarations, source or artifact access,
workflow persistence, or telemetry. Its `worker` child owns only the planning
request, configuration validation, requested-scope privacy, and deterministic
planner for scanner-worker targets.

## Exported surface

None. This parent is documentation-only. See the `worker` child for
`PlanRequest`, `WorkPlanner`, and `PlanScannerWorkerWork`.

## Dependencies

None. The parent `doc.go` contains only package documentation and its package
clause. Dependencies belong to leaf packages.

## Telemetry

None. This namespace executes no runtime code and changes no observability
contract.

## Gotchas / invariants

- Keep runtime declarations in leaf packages or the coordinator root.
- Keep scheduling, clocks, gates, durable admission, retries, queues, leases,
  and telemetry behavior in the coordinator root.
- The `worker` leaf is an in-process planner boundary, not an independently
  deployable or extractable service. Its collector analyzer and target-kind
  enum dependencies remain future extraction work.

## Related docs

- `go/internal/coordinator/scanner/worker/README.md`
- `go/internal/coordinator/README.md`
- `docs/internal/design/package-restructure.md`
