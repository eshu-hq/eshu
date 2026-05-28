# AWS Batch SDK Adapter

## Purpose

`internal/collector/awscloud/services/batch/awssdk` adapts the AWS SDK for Go
v2 Batch client into the scanner-owned records the Batch scanner consumes. It
owns pagination, batched describe calls, SDK-to-scanner mapping, and AWS API
telemetry for the Batch read surface.

## Ownership boundary

This package owns SDK pagination, describe batching, response mapping, throttle
detection, and pagination spans. It does not own fact emission, redaction
policy, credential acquisition, or graph projection. The scanner-owned domain
types live in the parent `batch` package.

## Exported surface

See `doc.go` for the godoc contract.

- `Client` - the SDK adapter implementing `batch.Client`.
- `NewClient` - constructs the adapter for one claimed AWS boundary.

The package depends on an unexported `apiClient` interface that names only the
List and Describe operations the adapter calls, so the metadata-only read
surface stays explicit and auditable.

## Dependencies

- `github.com/aws/aws-sdk-go-v2/service/batch` and its `types` package for the
  Batch client, paginators, and response shapes.
- `internal/collector/awscloud` for the boundary, API-call recording, and the
  shared throttle/telemetry helpers.
- `internal/collector/awscloud/services/batch` for the scanner-owned record
  types this adapter produces.
- `internal/telemetry` for the AWS API-call counters and pagination span name.

## Telemetry

The adapter records one `aws.service.pagination.page` span per API call and
increments `eshu_dp_aws_api_calls_total` (and `eshu_dp_aws_throttle_total` on
throttles) with bounded service, account, region, operation, and result
labels. It never puts ARNs, environment values, secret references, or tags into
metric labels.

## Gotchas / invariants

- The accepted `apiClient` interface excludes SubmitJob, CancelJob,
  TerminateJob, RegisterJobDefinition, and every Create/Update/Delete
  operation. `exclusion_test.go` reflects over the interface and fails the
  build if a mutation or job-control method ever becomes reachable.
- `mapJobDefinition` and `mapContainer` never read `ContainerProperties.Command`
  or `JobDefinition.Parameters`, so the container command list and job
  parameters cannot be persisted. The scanner-owned `Container` and
  `JobDefinition` types do not declare those fields.
- `mapSchedulingPolicy` discards `FairsharePolicy` and `QuotaSharePolicy`; only
  the policy ARN and name survive.
- `mapJob` maps job summaries to identity, status, and job-definition reference
  only; container overrides, array properties, and capacity usage are dropped.
- `ListRecentJobs` lists active job states (SUBMITTED, PENDING, RUNNABLE,
  STARTING, RUNNING) per queue, bounded by `recentJobsPerStatus`, and
  deduplicates by job ID across states so the scan stays bounded and current.
- Environment values are mapped verbatim only so the scanner can replace them
  with redaction markers; the adapter never persists them.

## Evidence

Collector Performance Evidence: `go test ./internal/collector/awscloud/services/batch/...`
covers the bounded Batch metadata path: one paginated
DescribeComputeEnvironments stream, one paginated DescribeJobQueues stream, one
paginated DescribeJobDefinitions stream filtered to ACTIVE definitions, one
paginated ListSchedulingPolicies stream followed by chunked
DescribeSchedulingPolicies calls bounded by `describeJobDefinitionsLimit`, and a
per-queue ListJobs fan-out bounded by `recentJobsPerStatus` across five active
states. No mutation or job-control API is reachable, and the collector performs
no graph writes.

No-Regression Evidence: `go test ./cmd/collector-aws-cloud ./internal/collector/awscloud/...`
covers compute-environment, job-queue, job-definition, scheduling-policy, and
recent-job fact emission, IAM-role/subnet/security-group/launch-template and
container-image relationship emission with non-empty target types, redaction of
container environment values, structural absence of command lists and job
parameters, scheduling-policy fair-share-state exclusion, runtime registration,
and command configuration. The adapter reflection contract test proves the
mutation and job-control APIs are unreachable.

Collector Observability Evidence: Batch uses the existing AWS collector
`aws.service.pagination.page` span plus `eshu_dp_aws_api_calls_total`,
`eshu_dp_aws_throttle_total`,
`eshu_dp_aws_resources_emitted_total{service="batch"}`,
`eshu_dp_aws_relationships_emitted_total`, and `aws_scan_status` rows. Metric
labels stay bounded to service, account, region, operation, result, and
resource type.

No-Observability-Change: the existing AWS collector telemetry contract already
diagnoses Batch scans through `aws.service.scan`, `aws.service.pagination.page`,
API/throttle counters, resource/relationship counters, and `aws_scan_status`.
No new instrument or label was added.

## Related docs

- `../README.md` for the Batch scanner contract.
- `../../../awsruntime/README.md` for the runtime surface.
- `docs/public/services/collector-aws-cloud-scanners.md` for the user-facing
  coverage table.
