# AWS DynamoDB Scanner

## Purpose

`dynamodb` converts Amazon DynamoDB control-plane table metadata into AWS cloud
collector facts and direct KMS relationship evidence.

## Ownership boundary

This package owns scanner-level table fact selection, identity mapping, and
metadata-only attribute shaping. It does not own AWS SDK pagination, credential
loading, workflow claims, fact persistence, graph writes, reducer admission,
workload ownership, or query behavior.

## Exported surface

Use `doc.go` and `go doc ./internal/collector/awscloud/services/dynamodb` for
the godoc contract. The main surfaces are the scanner, its minimal client
interface, snapshots, table metadata shape, and nested table attribute models.

## Dependencies

`dynamodb` depends on `internal/collector/awscloud` for boundaries, resource and
relationship constants, warning observations, and envelope builders. It uses
`internal/facts` for emitted fact envelopes.

## Telemetry

This scanner emits no spans or logs directly. `awsruntime.ClaimedSource`
records scan duration and emitted resource counts. The AWS SDK adapter records
DynamoDB API calls, throttles, and pagination spans.

## Gotchas / invariants

- The scanner must not read table items, stream records, exports, backup
  payloads, resource policies, PartiQL output, or mutate DynamoDB resources.
- Table metadata, tags, TTL, stream settings, capacity, table class, replicas,
  and backup status are reported control-plane evidence.
- Sustained throttling on optional TTL reads emits an `aws_warning`, leaves
  table facts present, and omits TTL metadata for that scan.
- Tags are raw AWS evidence. Do not infer environment, owner, workload,
  repository, or deployable-unit truth here.
- Direct KMS relationships are join evidence only; reducer correlation decides
  what becomes graph truth.

## Related docs

- `docs/public/services/collector-aws-cloud.md`
- `docs/public/guides/collector-authoring.md`
