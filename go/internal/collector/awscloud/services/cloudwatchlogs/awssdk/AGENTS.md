# AGENTS.md - internal/collector/awscloud/services/cloudwatchlogs/awssdk guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `client.go` - DescribeLogGroups pagination, tag reads, API telemetry, and
   throttle accounting.
3. `mapper.go` - AWS SDK shape to scanner-owned metadata mapping.
4. `../README.md` - scanner-level CloudWatch Logs fact contract.
5. `../../../awsruntime/README.md` - runtime registry and claim contract.
6. `docs/docs/adrs/2026-04-20-aws-cloud-scanner-collector.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep the API surface metadata-only: `DescribeLogGroups` and
  `ListTagsForResource`.
- Never call log event APIs, log stream payload APIs, Insights query APIs,
  export payload APIs, resource-policy APIs, subscription payload APIs, or
  mutation APIs.
- Set `Limit=50` and follow `NextToken` for `DescribeLogGroups`.
- Use the non-wildcard log group ARN for `ListTagsForResource`; trim a trailing
  `:*` from `arn` when `logGroupArn` is absent.
- Record every AWS SDK call with bounded telemetry labels only.
- Keep log group names, ARNs, tags, KMS key IDs, and raw AWS error payloads out
  of metric labels.

## Common Changes

- Add a new safe metadata field by updating `mapper.go` and writing an adapter
  test that proves data-plane fields are still dropped.
- Add a new CloudWatch Logs API call only after checking official AWS docs and
  updating the scanner package README with the metadata-only reason.
- Update `client.go` API-call telemetry whenever pagination or tag reads
  change.

## What Not To Change Without An ADR

- Do not add CloudWatch Logs mutation APIs, log event reads, log stream payload
  reads, Insights query calls, export payload reads, resource-policy reads,
  subscription payload reads, or graph writes.
- Do not move credential acquisition, STS, or target authorization into this
  package.
