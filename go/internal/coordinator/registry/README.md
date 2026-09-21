# Coordinator package-registry contracts

## Purpose

`coordinator/registry` groups the coordinator-side contracts used to plan
package-registry collection. It is a documentation-only namespace;
implementation belongs in its leaf packages and runtime behavior stays in the
coordinator root.

## Ownership boundary

The namespace owns no runtime declarations, scheduling, registry access,
workflow persistence, or telemetry. Its `package` child owns the planning
request, the deterministic planner, and the owned-dependency derivation
settings for configured package-registry targets.

## Exported surface

None. This parent is documentation-only. See the `package` child for
`PlanRequest`, `WorkPlanner`, `DerivationConfiguration`,
`DerivationFromConfig`, `DerivationEcosystems`, and `DerivedTargetLimit`.

## Dependencies

None. The parent `doc.go` contains only package documentation and its package
clause. Dependencies belong to leaf packages.

## Telemetry

None. This namespace executes no runtime code and changes no observability
contract.

## Gotchas / invariants

- Keep runtime declarations in leaf packages or the coordinator root.
- The `package` leaf's directory name is the `package` keyword, so its Go
  package name is `packages` and importers must alias it:
  `packages "github.com/eshu-hq/eshu/go/internal/coordinator/registry/package"`.
  The root does this in `package_registry_service.go`.
- The root's `package_registry_service.go` does not collide with this
  directory name under dirgate: the sibling name is `registry`, and the file
  stem neither equals it nor begins with `registry_`.
- The `package` leaf must not import the coordinator root.

## Related docs

- `go/internal/coordinator/registry/package/README.md`
- `go/internal/coordinator/README.md`
- `docs/internal/design/package-restructure.md`
