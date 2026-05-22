# AWS ECR SDK Adapter

## Purpose

`internal/collector/awscloud/services/ecr/awssdk` adapts AWS SDK for Go v2 ECR
responses to the scanner-owned `ecr.Client` contract. It owns ECR API
pagination, durable image pagination checkpoint use, repository tag reads,
lifecycle policy reads, throttle classification, and per-call AWS API
telemetry.

## Ownership boundary

This package owns SDK calls for ECR. It does not own workflow claims,
credential acquisition, ECR fact selection, graph writes, reducer admission, or
query behavior.

## Exported surface

See `doc.go` for the godoc contract.

- `Client` - AWS SDK-backed implementation of `ecr.Client`.
- `NewClient` - builds a `Client` for one claimed AWS boundary.
- `NewClientWithCheckpoints` - builds a `Client` with a durable checkpoint
  store for long `DescribeImages` scans.

## Dependencies

The adapter imports the AWS SDK for Go v2 ECR client, Smithy API errors,
claim-fenced checkpoint storage, AWS boundary/status helpers, scanner-owned ECR
result types, and shared AWS telemetry.

## Telemetry

ECR paginator pages and point reads record `aws.service.pagination.page`,
`eshu_dp_aws_api_calls_total`, and `eshu_dp_aws_throttle_total`. Metric labels
stay bounded to service, account, region, operation, and result. Repository
ARNs, tags, lifecycle policy JSON, image digests, and raw AWS error payloads
stay out of metric labels.

## Gotchas / invariants

- `DescribeImages` is used for image pagination because it returns digest, tag,
  pushed-at, size, and media-type metadata in one paged source.
- `DescribeImages` checkpoints store the retry-safe page token before each
  page read. A crash may re-read the last page, but it must not skip image
  facts whose generation transaction may not have committed.
- Repeated image pages are deduped in memory before returning scanner-owned
  image records.
- `LifecyclePolicyNotFoundException` is a valid empty policy result.
- `ListTagsForResource` is called per repository because `DescribeRepositories`
  does not return repository tags.
- SDK adapters translate AWS records into scanner-owned types; scanner tests
  should not mock AWS SDK paginators.

## Related docs

- `docs/public/services/collector-aws-cloud.md`
- `docs/public/guides/collector-authoring.md`
