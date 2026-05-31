# AGENTS.md - internal/collector/awscloud/services/keyspaces/awssdk guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `client.go` - Keyspaces pagination, point reads, API telemetry, and throttle
   accounting.
3. `mapper.go` - AWS SDK shape to scanner-owned metadata mapping.
4. `../README.md` - scanner-level Keyspaces fact contract.
5. `../../../awsruntime/README.md` - runtime registry and claim contract.
6. `docs/public/services/collector-aws-cloud.md` - AWS collector service
   coverage and runtime requirements.

## Invariants

- Keep the API surface metadata-only: `ListKeyspaces`, `GetKeyspace`,
  `ListTables`, `GetTable`, and `ListTagsForResource`. The `apiClient` interface
  is the enforced boundary; the exclusion reflection test fails if any other
  method is added.
- Never call `ExecuteStatement`, `BatchStatement`, any CQL `Select`, row/cell
  reads, `RestoreTable`, or any keyspace/table mutation API.
- Set `MaxResults=100` and follow `NextToken` for `ListKeyspaces`, `ListTables`,
  and `ListTagsForResource`.
- Attach the parent keyspace's API `ResourceArn` to each table so the
  table-in-keyspace edge joins the keyspace node by its published ARN.
- Map structural schema (column names, types, partition/clustering/static
  columns) only; never map or read table rows, cells, or query results.
- Record every AWS SDK call with bounded telemetry labels only.
- Keep keyspace names, table names, ARNs, tags, schema columns, KMS key IDs, and
  raw AWS error payloads out of metric labels.

## Common Changes

- Add a new safe metadata field by updating `mapper.go` and writing an adapter
  test that proves data-plane fields are still dropped.
- Add a new Keyspaces API call only after checking official AWS docs, confirming
  it is read-only metadata, updating the `apiClient` interface and the exclusion
  test's allowed set, and updating the scanner package README.
- Update `client.go` API-call telemetry whenever pagination or point reads
  change.

## What Not To Change Without An ADR

- Do not add Keyspaces mutation APIs, CQL execution, row/cell reads,
  `RestoreTable` calls, or graph writes.
- Do not move credential acquisition, STS, or target authorization into this
  package.
