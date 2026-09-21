# Coordinator governance contracts

## Purpose

`coordinator/governance` groups the coordinator-side contracts used to record
governance audit evidence. It is a documentation-only namespace;
implementation belongs in its leaf packages and runtime behavior stays in the
coordinator root.

## Ownership boundary

The namespace owns no runtime declarations, scheduling, appender wiring,
workflow persistence, or telemetry. Its `audit` child owns only the audit
event shape and the deterministic identity helpers that derive an event's hash
and correlation ID.

The appender interface is deliberately not declared here. Each consumer
declares its own: the coordinator root in `governance_audit.go`, and
`semantic` in `provider_worker.go`. That keeps `audit` free of a dependency on
either consumer and follows the repository's consumer-defined-interface rule.

## Exported surface

None. This parent is documentation-only. See the `audit` child for `Event`,
`Hash`, `CorrelationID`, `ServiceID`, and `AppendTimeout`.

## Dependencies

None. The parent `doc.go` contains only package documentation and its package
clause. Dependencies belong to leaf packages.

## Telemetry

None. This namespace executes no runtime code and changes no observability
contract.

## Gotchas / invariants

- Keep runtime declarations in leaf packages or the coordinator root.
- `audit.Hash` and `audit.CorrelationID` decide durable audit identity. Two
  call sites passing the same prefix with different arguments, or the same
  arguments in a different order, silently split that identity and no existing
  test fails. Change either function only with every call site re-checked.
- Adding a direct `.go` file to this directory makes `governance` a naming
  subpackage for the dirgate linter, which then requires the root
  `governance_audit.go` to carry a `//nolint:dirgate` marker. That marker is
  in place; do not remove it while this `doc.go` exists.
- The `audit` leaf must not import the coordinator root.

## Related docs

- `go/internal/coordinator/governance/audit/README.md`
- `go/internal/coordinator/README.md`
- `docs/internal/design/package-restructure.md`
