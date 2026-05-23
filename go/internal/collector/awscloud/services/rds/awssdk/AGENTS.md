# AGENTS.md - internal/collector/awscloud/services/rds/awssdk guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `client.go` - RDS pagination, tag reads, API telemetry, and throttle
   accounting.
3. `mapper.go` - AWS SDK shape to scanner-owned metadata mapping.
4. `../README.md` - scanner-level RDS fact contract.
5. `../../../awsruntime/README.md` - runtime registry and claim contract.
6. `docs/docs/adrs/2026-04-20-aws-cloud-scanner-collector.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep the API surface metadata-only: `DescribeDBInstances`,
  `DescribeDBClusters`, `DescribeDBSubnetGroups`, and `ListTagsForResource`.
- Never call RDS log download, snapshot, Performance Insights, data-plane, or
  mutation APIs.
- Drop database names, master usernames, secrets, snapshots, log payloads,
  schemas, tables, and row data during mapping.
- Set `MaxRecords=100` and follow `Marker` pagination for RDS describe calls.
- Record every AWS SDK call with bounded telemetry labels only.
- Keep ARNs, endpoints, tags, KMS key IDs, subnet group names, and raw AWS error
  payloads out of metric labels.

## Common Changes

- Add a new safe metadata field by updating `mapper.go` and writing an adapter
  test that proves sensitive RDS fields are still dropped.
- Add a new RDS API call only after checking official AWS docs and updating the
  scanner package README with the metadata-only reason.
- Update `client.go` API-call telemetry whenever pagination or tag reads change.

## What Not To Change Without An ADR

- Do not add RDS mutation APIs, snapshot reads, log reads, data-plane calls, or
  graph writes.
- Do not move credential acquisition, STS, or target authorization into this
  package.
