# AGENTS.md - internal/collector/awscloud/services/docdb/awssdk guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `client.go` - DocumentDB pagination, parameter counting, tag reads, API
   telemetry, and throttle accounting.
3. `mapper.go` - AWS SDK shape to scanner-owned metadata mapping.
4. `../README.md` - scanner-level DocumentDB fact contract.
5. `../../../awsruntime/README.md` - runtime registry and claim contract.
6. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep the API surface metadata-only: `DescribeDBClusters`,
  `DescribeDBInstances`, `DescribeDBClusterParameterGroups`,
  `DescribeDBClusterParameters`, `DescribeDBClusterSnapshots`,
  `DescribeDBSubnetGroups`, `DescribeGlobalClusters`,
  `DescribeEventSubscriptions`, and `ListTagsForResource`.
- Never call DocumentDB mutation, snapshot-write, failover, reboot, restore, or
  data-plane APIs.
- `DescribeDBClusterParameters` counts parameters only. Never copy a parameter
  name or value into a scanner result.
- Drop master usernames, master user secrets, database document contents, and
  snapshot contents during mapping.
- Set `MaxRecords=100` and follow `Marker` pagination for DocumentDB describe
  calls.
- Record every AWS SDK call with bounded telemetry labels only.
- Keep ARNs, endpoints, tags, KMS key IDs, subnet group names, parameter
  names/values, and raw AWS error payloads out of metric labels.

## Common Changes

- Add a new safe metadata field by updating `mapper.go` and writing an adapter
  test that proves sensitive DocumentDB fields are still dropped.
- Add a new DocumentDB API call only after checking official AWS docs and
  updating the scanner package README with the metadata-only reason. Add the
  matching method to the `apiClient` interface and `fake_test.go`.
- Update `client.go` API-call telemetry whenever pagination or tag reads change.

## What Not To Change Without An ADR

- Do not add DocumentDB mutation APIs, snapshot-write APIs, parameter-value
  reads, data-plane calls, or graph writes.
- Do not move credential acquisition, STS, or target authorization into this
  package.
