# AWS IAM projector intents

## Purpose

This documentation-only namespace groups AWS IAM reducer-intent builders.

## Ownership boundary

`trust` selects CAN_ASSUME trust statements. `instance/profile` selects
instance-profile observations for HAS_ROLE reconciliation. Neither writes
graph edges or owns the projector queue.

## Exported surface

None. Import the owning leaf directly.

## Dependencies

None in this namespace.

## Telemetry

None here. Root projector and reducer retain their existing signals.

## Gotchas / invariants

Both leaves preserve the shared `aws_resource_materialization:<scope>`
readiness key; do not make it family-specific during a path move.

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

No-Observability-Change: this namespace package has no runtime
implementation. Existing projector enqueue/run metrics, reducer execution
metrics and spans, readiness status, and materialization logs retain their
names and owners.
