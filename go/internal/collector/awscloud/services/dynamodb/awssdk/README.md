# AWS DynamoDB SDK Adapter

## Purpose

`internal/collector/awscloud/services/dynamodb/awssdk` adapts AWS SDK for Go v2
DynamoDB responses to the scanner-owned `Client` contract. It owns DynamoDB
pagination, table metadata point reads, resource tag reads, TTL and continuous
backup metadata reads, throttle classification, and per-call AWS API telemetry.

## Ownership boundary

This package owns SDK calls for DynamoDB. It does not own workflow claims,
credential acquisition, DynamoDB fact selection, graph writes, reducer
admission, workload ownership, or query behavior.

## Exported surface

See `doc.go` for the godoc contract.

- `Client` - AWS SDK-backed implementation of `dynamodb.Client`, including
  snapshot warnings for partial optional metadata coverage.
- `NewClient` - builds a `Client` for one claimed AWS boundary.

## Dependencies

The adapter imports the AWS SDK for Go v2 DynamoDB client, Smithy API errors,
`internal/collector/awscloud` boundary/status helpers, scanner-owned DynamoDB
result types, and shared AWS telemetry.

## Telemetry

DynamoDB list pages, point reads, and tag pages record
`aws.service.pagination.page`, `eshu_dp_aws_api_calls_total`, and
`eshu_dp_aws_throttle_total`. Metric labels stay bounded to service, account,
region, operation, and result. Table names, ARNs, tags, index names, KMS key
IDs, TTL attribute names, and raw AWS error payloads stay out of metric labels.

## Gotchas / invariants

- The adapter calls only `ListTables`, `DescribeTable`, `ListTagsOfResource`,
  `DescribeTimeToLive`, and `DescribeContinuousBackups`.
- `ListTables` sets `Limit=100`, the documented maximum, and follows
  `LastEvaluatedTableName`.
- `ListTagsOfResource` is called only when AWS returned a table ARN and follows
  `NextToken`.
- `DescribeTimeToLive` has a lower service quota than the general DynamoDB
  read-only control-plane path. If it is throttled after SDK retries, the
  adapter emits one `throttle_sustained` warning and leaves TTL metadata empty
  instead of failing the entire table scan. The adapter skips further TTL
  lookups for the same scan after the first sustained TTL throttle so large
  account scans do not burn one retry budget per table.
- The adapter maps safe control-plane fields and drops item values, table scan
  results, query results, stream records, backup/export payloads, resource
  policies, and mutation surfaces.
- The adapter must not call `Scan`, `Query`, `GetItem`, `BatchGetItem`,
  `ExecuteStatement`, DynamoDB Streams record APIs, export/backup payload APIs,
  resource-policy APIs, or mutation APIs.

## Related docs

- `docs/public/services/collector-aws-cloud.md`
- `docs/public/guides/collector-authoring.md`
