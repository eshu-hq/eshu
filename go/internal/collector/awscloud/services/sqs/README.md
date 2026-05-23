# AWS SQS Scanner

## Purpose

`internal/collector/awscloud/services/sqs` owns the SQS scanner contract for the
AWS cloud collector. It converts queue metadata into `aws_resource` facts and
emits optional dead-letter queue relationship evidence from safe redrive
attributes.

## Ownership boundary

This package owns scanner-level SQS fact selection and identity mapping. It does
not own AWS SDK pagination, STS credentials, workflow claims, fact persistence,
graph writes, reducer admission, or query behavior.

```mermaid
flowchart LR
  A["SQS API adapter"] --> B["Client"]
  B --> C["Scanner.Scan"]
  C --> D["aws_resource"]
  C --> E["aws_relationship"]
  D --> F["facts.Envelope"]
  E --> F
```

## Exported surface

See `doc.go` for the godoc contract.

- `Client` - minimal SQS metadata read surface consumed by `Scanner`.
- `Scanner` - emits queue metadata facts for one boundary.
- `Queue` - scanner-owned SQS queue representation.
- `QueueAttributes` - safe queue metadata fields. Message bodies and queue
  policy JSON are intentionally outside the contract.

## Dependencies

- `internal/collector/awscloud` for boundaries, resource constants,
  relationship constants, and envelope builders.
- `internal/facts` for emitted fact envelope kinds.

The package depends on a small `Client` interface rather than the AWS SDK for Go
v2 so tests can use fake clients and runtime adapters can own SDK behavior.

## Telemetry

This scanner emits no spans or logs directly. `awsruntime.ClaimedSource`
records scan duration and emitted resource counts after `Scanner.Scan` returns.
The `awssdk` adapter records SQS API call counts, throttles, and pagination
spans.

## Gotchas / invariants

- SQS queue facts are metadata only. The scanner must not read messages or
  persist message bodies.
- Queue policy JSON is not persisted because it is authorization policy data,
  not inventory metadata.
- Dead-letter queue relationships are emitted only when the source queue ARN
  and `deadLetterTargetArn` are both present.
- Tags are raw AWS tag evidence. Do not infer environment, owner, workload, or
  deployable-unit truth from tags in this package.

## Evidence

Collector Performance Evidence: `go test ./internal/collector/awscloud/services/sqs/...`
covers the bounded SQS metadata path: one paginated queue listing, one metadata
attribute read per queue, one tag read per queue, no message reads, and no queue
mutations.

No-Regression Evidence: `go test ./cmd/collector-aws-cloud ./internal/collector/awscloud/...`
covers SQS queue metadata fact emission, dead-letter queue relationship
emission, safe omission of queue policy JSON, runtime registration, command
configuration, and the SDK adapter's explicit metadata attribute allowlist.

Collector Observability Evidence: SQS uses the existing AWS collector
`aws.service.pagination.page` span plus `eshu_dp_aws_api_calls_total`,
`eshu_dp_aws_throttle_total`, `eshu_dp_aws_resources_emitted_total`,
`eshu_dp_aws_relationships_emitted_total`, and `aws_scan_status` rows. Metric
labels stay bounded to service, account, region, operation, result, and status.

No-Observability-Change: the existing AWS collector telemetry contract already
diagnoses SQS scans through `aws.service.scan`, `aws.service.pagination.page`,
API/throttle counters, resource/relationship counters, and `aws_scan_status`.

Collector Deployment Evidence: SQS runs inside the existing hosted
`collector-aws-cloud` runtime, so `/healthz`, `/readyz`, `/metrics`, and
`/admin/status` stay covered by the command wiring and Helm collector runtime.

## Related docs

- `docs/docs/adrs/2026-04-20-aws-cloud-scanner-collector.md`
- `docs/docs/guides/collector-authoring.md`
