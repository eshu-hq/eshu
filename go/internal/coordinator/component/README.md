# Coordinator component contracts

## Purpose

`coordinator/component` groups dependency-neutral contracts used by component
orchestration. It is a documentation-only namespace; runtime behavior stays in
the coordinator root and implementation belongs in its leaf packages.

## Ownership boundary

The namespace owns no runtime declarations, scheduling, component registry
access, workflow persistence, or telemetry. Its `activation` child owns the
shared configuration shape and validation rules used by coordinator root and
the component-extension planner.

## Exported surface

None. This parent is documentation-only. See the `activation` child for
`ConfigSchema`, `Config`, `RuntimeConfig`, and `ParseConfig`.

## Dependencies

None. The parent `doc.go` contains only package documentation and its package
clause. Dependencies belong to leaf packages.

## Telemetry

None. This namespace executes no runtime code and changes no observability
contract.

## Gotchas / invariants

- Keep runtime declarations in leaf packages or the coordinator root.
- Keep shared component contracts outside `planner/component`; root and the
  extension planner must be able to import them without a cycle.
- A leaf must not import the coordinator root or a sibling package unless its
  ownership and dependency direction are explicitly redesigned.

## Related docs

- `go/internal/coordinator/component/activation/README.md`
- `go/internal/coordinator/README.md`
- `docs/internal/design/naming-remediation.md`
