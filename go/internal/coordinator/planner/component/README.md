# Component Planners

## Purpose

`internal/coordinator/planner/component` groups coordinator planning for
component-owned workflow families. The current child plans generic component
extension work.

## Ownership boundary

The `extension` leaf validates a planning request, delegates parsing of the
collector instance's `Configuration` string to
`activation.ParseConfig`, and builds deterministic workflow rows from
the resulting configuration. The shared configuration contract remains in
`internal/coordinator/component/activation` because root construction, policy,
and audit paths also consume it.

## Exported surface

None. Import `component/extension` directly.

See `doc.go` for the package contract.

## Dependencies

None. The namespace package contains documentation only.

## Telemetry

None. Component scheduling and policy signals remain in the coordinator root.

## Gotchas / invariants

- Do not move `component/activation` into this namespace.
- The extension leaf must not import the parent coordinator package.
- Keep registry access, egress policy, durable admission, retries, and
  telemetry outside the planner leaf.

## Related docs

- `go/internal/coordinator/planner/component/extension/README.md`
- `go/internal/coordinator/component/activation/README.md`
