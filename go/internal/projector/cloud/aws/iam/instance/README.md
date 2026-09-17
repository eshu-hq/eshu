# AWS IAM instance projector intents

## Purpose

This documentation-only namespace groups instance-related AWS IAM intents.

## Ownership boundary

The `profile` leaf admits profile observations for reducer-owned HAS_ROLE
reconciliation. Root projector keeps lookup, fan-out, and queue ownership.

## Exported surface

None. Import `profile` directly.

## Dependencies

None in this namespace.

## Telemetry

None here. Root projector and reducer retain their existing signals.

## Gotchas / invariants

An empty role list must still trigger the profile leaf so stale edges retract.

### Move record (#6627)

No-Regression Evidence: this move relocates the `instance/profile` builder
(with its decode seam) from `internal/projector/iaminstanceprofile` without
changing trigger selection, intent values, or root fan-out order.
Base: `6abf61fe4`. Backend: not applicable; proof is in-process
(`go build ./...`, `go vet`, `go test ./internal/projector/...`
`./internal/mcp/... -count=1`). Census: this builder consumes only the
AWS-only `AWSResourceFactKind` filtered to
`resource_type aws_iam_instance_profile`, zero GCP/Azure references.

No-Observability-Change: this namespace package has no runtime
implementation. Existing projector enqueue/run metrics, reducer execution
metrics and spans, readiness status, and materialization logs retain their
names and owners.
