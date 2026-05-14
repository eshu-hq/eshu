# AGENTS.md - internal/collector/awscloud/services/dynamodb/awssdk guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `client.go` - DynamoDB pagination, point reads, API telemetry, and throttle
   accounting.
3. `mapper.go` - AWS SDK shape to scanner-owned metadata mapping.
4. `../README.md` - scanner-level DynamoDB fact contract.
5. `../../../awsruntime/README.md` - runtime registry and claim contract.
6. `docs/docs/adrs/2026-04-20-aws-cloud-scanner-collector.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep the API surface metadata-only: `ListTables`, `DescribeTable`,
  `ListTagsOfResource`, `DescribeTimeToLive`, and
  `DescribeContinuousBackups`.
- Never call DynamoDB item APIs, table scan/query APIs, stream record APIs,
  export/backup payload APIs, resource-policy APIs, PartiQL APIs, or mutation
  APIs.
- Set `Limit=100` and follow `LastEvaluatedTableName` for `ListTables`.
- Follow `NextToken` for `ListTagsOfResource`.
- Record every AWS SDK call with bounded telemetry labels only.
- Keep table names, ARNs, tags, index names, KMS key IDs, TTL attribute names,
  and raw AWS error payloads out of metric labels.

## Common Changes

- Add a new safe metadata field by updating `mapper.go` and writing an adapter
  test that proves data-plane fields are still dropped.
- Add a new DynamoDB API call only after checking official AWS docs and updating
  the scanner package README with the metadata-only reason.
- Update `client.go` API-call telemetry whenever pagination, point reads, or
  tag reads change.

## What Not To Change Without An ADR

- Do not add DynamoDB mutation APIs, item reads, table scans, queries, stream
  record reads, backup/export payload reads, resource-policy reads, PartiQL
  calls, or graph writes.
- Do not move credential acquisition, STS, or target authorization into this
  package.
