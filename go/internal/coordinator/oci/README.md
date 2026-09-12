# Coordinator OCI contracts

## Purpose

`coordinator/oci` groups coordinator-side contracts used to plan OCI work. It
is a documentation-only namespace; implementation belongs in its leaf packages
and runtime behavior stays in the coordinator root.

## Ownership boundary

The namespace owns no runtime declarations, registry access, workflow
persistence, or telemetry. Its `registry` child owns only the planning request,
configured-target normalization, and deterministic planner for OCI repository
targets.

## Exported surface

None. This parent is documentation-only. See the `registry` child for
`PlanRequest`, `WorkPlanner`, and `PlanOCIRegistryWork`.

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
- The `registry` leaf is an in-process planner boundary, not an independently
  deployable service. Its collector identity and provider-adapter dependencies
  remain future extraction work.

## Related docs

- `go/internal/coordinator/oci/registry/README.md`
- `go/internal/coordinator/README.md`
- `docs/internal/design/package-restructure.md`
