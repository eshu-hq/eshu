# AGENTS.md - internal/collector/awscloud/services/neptune/awssdk guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `client.go` - Neptune (provisioned) pagination, tag reads, API telemetry,
   throttle accounting, and the two SDK-surface interfaces.
3. `graph_client.go` - Neptune Analytics graph and graph-snapshot reads.
4. `mapper.go` - AWS SDK shape to scanner-owned metadata mapping.
5. `../README.md` - scanner-level Neptune fact contract.
6. `../../../awsruntime/README.md` - runtime registry and claim contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep the Neptune (provisioned) API surface metadata-only:
  `DescribeDBClusters`, `DescribeDBInstances`,
  `DescribeDBClusterParameterGroups`, `DescribeDBClusterSnapshots`,
  `DescribeDBSubnetGroups`, `DescribeGlobalClusters`, and
  `ListTagsForResource`.
- Keep the Neptune Analytics API surface metadata-only: `ListGraphs`,
  `GetGraph`, `ListGraphSnapshots`, and `ListTagsForResource`.
- Never call Neptune mutation, snapshot-write, failover, reboot, or restore
  APIs. Never call any Neptune Analytics graph data-plane API (ExecuteQuery,
  CancelQuery, GetQuery, ListQueries), graph mutation, or import/export task
  API. The interface shapes make these unreachable.
- Never read or count cluster parameter values. The adapter does not call
  `DescribeDBClusterParameters`.
- `GetGraph` reads control-plane detail only (vector-search dimension, KMS
  key). Never read graph vertex, edge, or query data.
- Drop master usernames and master user secrets during mapping.
- Set `MaxRecords=100` and follow `Marker` pagination for provisioned describe
  calls. Set `MaxResults=100` and follow `NextToken` for Neptune Analytics
  list calls.
- Record every AWS SDK call with bounded telemetry labels only.
- Keep ARNs, endpoints, tags, KMS key IDs, subnet group names, parameter group
  names, graph identifiers, and raw AWS error payloads out of metric labels.

## Common Changes

- Add a new safe metadata field by updating `mapper.go` and writing an adapter
  test that proves sensitive Neptune fields are still dropped.
- Add a new Neptune API call only after checking official AWS docs and updating
  the scanner package README with the metadata-only reason. Add the matching
  method to the relevant `neptuneAPI` or `neptuneGraphAPI` interface and
  `fake_test.go`.
- Update `client.go` or `graph_client.go` API-call telemetry whenever
  pagination or tag reads change.

## What Not To Change Without An ADR

- Do not add Neptune mutation APIs, snapshot-write APIs, parameter-value reads,
  Neptune Analytics graph data-plane calls, import/export task calls, or Eshu
  graph writes.
- Do not move credential acquisition, STS, or target authorization into this
  package.
