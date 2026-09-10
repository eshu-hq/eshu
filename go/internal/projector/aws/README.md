# AWS Projector Intents

## Purpose

`internal/projector/aws` groups AWS-specific reducer-intent builders under a
responsibility-first path.

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
- AWS leaf builders depend on `internal/projector/intent`, not the parent
  projector package.
- Keep trigger facts, reducer domains, entity keys, reasons, source-system
  derivation, and root fan-out positions stable during path-only moves.

## Child packages

- `cloud/image` builds the AWS Lambda-to-container-image materialization
  intent.
- `ec2`, `rds`, and `s3` build service-specific posture and relationship
  intents.
- `relationship` and `resource` build provider-wide AWS graph
  materialization intents.

## Related docs

- `go/internal/projector/README.md`
- `docs/internal/design/naming-remediation.md`
