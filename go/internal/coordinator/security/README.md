# Coordinator security contracts

## Purpose

`coordinator/security` groups the coordinator-side contracts used to plan
provider security-alert collection. It is a documentation-only namespace;
implementation belongs in its leaf packages and runtime behavior stays in the
coordinator root.

## Ownership boundary

The namespace owns no runtime declarations, scheduling, artifact access,
workflow persistence, or telemetry. Its `alert` child owns only the
planning request and deterministic planner for configured provider
security-alert targets.

## Exported surface

None. This parent is documentation-only. See the `alert` child for
`PlanRequest`, `WorkPlanner`, and `PlanSecurityAlertWork`.

## Dependencies

None. The parent `doc.go` contains only package documentation and its package
clause. Dependencies belong to leaf packages.

## Telemetry

None. This namespace executes no runtime code and changes no observability
contract.

## Gotchas / invariants

- Keep runtime declarations in leaf packages or the coordinator root.
- Keep scheduling, clocks, gates, durable admission, retry, queue, lease, and
  telemetry behavior in the coordinator root.
- The `alert` leaf is a planner boundary, not an independently
  deployable service. It must not import the coordinator root.

## Related docs

- `go/internal/coordinator/security/alert/README.md`
- `go/internal/coordinator/README.md`
- `docs/internal/design/package-restructure.md`
