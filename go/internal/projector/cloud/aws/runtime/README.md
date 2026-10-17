# AWS runtime projector intents

## Purpose

`internal/projector/cloud/aws/runtime` groups AWS-specific reducer-intent
builders driven by observed cloud runtime state under a
responsibility-first path. `drift/` holds the AWS runtime-drift leaf.

## Ownership boundary

This directory is a namespace, not a runtime layer. Leaf packages select
trigger facts and return reducer-intent values. The parent
`internal/projector` package continues to own lookup construction, ordered
fan-out, projection lifecycle, queue writes, retries, and telemetry. Reducer
packages continue to own graph materialization.

## Exported surface

None. Import the leaf package that owns the required intent family.

See `doc.go` for the package contract.

## Dependencies

None. The namespace package contains documentation only.

## Telemetry

None. Leaf builders emit no telemetry; projector and reducer runtime signals
retain their existing ownership.

## Gotchas / invariants

- Do not put orchestration, shared mutable state, or provider-wide helpers in
  this namespace.
- AWS runtime leaves depend on `internal/projector/intent`, not the parent
  projector package.
- Keep trigger facts, reducer domains, entity keys, reasons, source-system
  derivation, and root fan-out positions stable during path-only moves.

## Child packages

- `drift` builds the AWS cloud-runtime-drift reducer intent from
  `aws_resource` fact presence.

## Related docs

- [Projector architecture](../../../README.md)
- [Intent contract](../../../intent/README.md)
- [Package restructure](../../../../../../docs/internal/design/package-restructure.md)
