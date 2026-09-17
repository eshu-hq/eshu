# AWS Projector Intents

## Purpose

`internal/projector/cloud/aws` groups AWS-specific reducer-intent builders under a
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

- `iam/trust` and `iam/instance/profile` build IAM edge intents.

## Related docs

- `go/internal/projector/README.md`
- `docs/internal/design/naming-remediation.md`

### Move record (#6627)

No-Regression Evidence: this move relocates the `trust` and
`instance/profile` builders (with their decode seams) from
`internal/projector/iamcanassume` and `internal/projector/iaminstanceprofile`
without changing trigger selection, intent values, or root fan-out order.
Base: `6abf61fe4`. Backend: not applicable; proof is in-process
(`go build ./...`, `go vet`, `go test ./internal/projector/...`
`./internal/mcp/... -count=1`). Census: both builders consume AWS-only fact
kinds (`AWSIAMPermissionFactKind`, and `AWSResourceFactKind` filtered to
`resource_type aws_iam_instance_profile`), zero GCP/Azure references.

No-Observability-Change: these namespace packages have no runtime
implementation. Existing projector enqueue/run metrics, reducer execution
metrics and spans, readiness status, and materialization logs retain their
names and owners.
