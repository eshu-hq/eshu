# AWS DynamoDB Scanner

## Purpose

`internal/collector/awscloud/services/dynamodb` owns the Amazon DynamoDB scanner
contract for the AWS cloud collector. It converts DynamoDB control-plane table
metadata into `aws_resource` facts and emits relationship evidence when
DynamoDB directly reports a server-side encryption KMS key identifier.

## Ownership boundary

This package owns scanner-level DynamoDB fact selection and identity mapping.
It does not own AWS SDK pagination, STS credentials, workflow claims, fact
persistence, graph writes, reducer admission, workload ownership, or query
behavior.

```mermaid
flowchart LR
  A["DynamoDB API adapter"] --> B["Client"]
  B --> C["Scanner.Scan"]
  C --> D["aws_resource"]
  C --> E["aws_relationship"]
  D --> F["facts.Envelope"]
  E --> F
```

## Exported surface

See `doc.go` for the godoc contract.

- `Client` - minimal DynamoDB metadata snapshot surface consumed by `Scanner`.
- `Snapshot` - table metadata plus non-fatal scan warnings for partial optional
  metadata coverage.
- `Scanner` - emits table metadata and direct KMS relationship facts for one
  boundary.
- `Table` - scanner-owned metadata-only table representation.
- `KeySchemaElement`, `AttributeDefinition`, `Throughput`,
  `OnDemandThroughput`, `SSE`, `TTL`, `ContinuousBackups`, `Stream`,
  `SecondaryIndex`, and `Replica` - table metadata shapes copied into safe
  resource attributes.

## Dependencies

- `internal/collector/awscloud` for boundaries, resource constants,
  relationship constants, and envelope builders.
- `internal/facts` for emitted fact envelope kinds.

The package depends on a small `Client` interface rather than the AWS SDK for Go
v2 so tests can use fake clients and runtime adapters can own SDK behavior.

## Telemetry

This scanner emits no spans or logs directly. `awsruntime.ClaimedSource`
records scan duration and emitted resource counts after `Scanner.Scan` returns.
The `awssdk` adapter records DynamoDB API call counts, throttles, and pagination
spans.

## Gotchas / invariants

- DynamoDB facts are metadata only. The scanner must not read table items,
  query or scan tables, read stream records, fetch exports, fetch backup
  payloads, fetch resource policies, run PartiQL, or mutate resources.
- Attribute definitions, key schema, index definitions, TTL attribute names,
  stream settings, capacity settings, table class, replicas, tags, and backup
  status are reported control-plane metadata.
- Sustained throttling on optional `DescribeTimeToLive` calls emits an
  `aws_warning` with `warning_kind=throttle_sustained`, leaves table facts
  present, and omits TTL metadata for the affected scan rather than failing the
  whole DynamoDB claim.
- Tags are raw AWS tag evidence. Do not infer environment, owner, workload,
  repository, or deployable-unit truth from tags in this package.
- The KMS relationship is reported join evidence only. Correlation belongs in
  reducers.

## Verification

```bash
go test ./internal/collector/awscloud/services/dynamodb/... -count=1
go test ./internal/collector/awscloud/awsruntime -run TestClaimedSourceMarksThrottleWarningAsPartial -count=1
go test ./cmd/collector-aws-cloud ./internal/collector/awscloud/... -count=1
go run ./cmd/eshu docs verify ../go/internal/collector/awscloud/services/dynamodb --limit 1000 \
  --fail-on contradicted,missing_evidence
```

Run the AWS runtime tests when scan warnings or partial-status behavior changes.

## Related docs

- `docs/public/services/collector-aws-cloud.md`
- `docs/public/guides/collector-authoring.md`
