# AGENTS.md - internal/collector/awscloud/services/redshift/awssdk guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `client.go` - Redshift and Redshift Serverless pagination, tag reads, API
   telemetry, and throttle accounting.
3. `mapper.go` - AWS SDK shape to scanner-owned metadata mapping.
4. `../README.md` - scanner-level Redshift fact contract.
5. `../../../awsruntime/README.md` - runtime registry and claim contract.
6. `docs/public/services/collector-aws-cloud.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep the API surface metadata-only: `DescribeClusters`,
  `DescribeClusterParameterGroups`, `DescribeClusterSubnetGroups`,
  `DescribeClusterSnapshots`, `DescribeScheduledActions`,
  `ListNamespaces`, `ListWorkgroups`, and `ListTagsForResource` (Serverless
  only - provisioned describe calls return tags inline).
- Never call Redshift or Redshift Serverless mutation APIs, data-plane query
  APIs, snapshot content reads, or admin-credential APIs.
- Drop master user names, admin user names, master password secret ARNs,
  master password KMS key IDs, raw target-action JSON, query results,
  snapshot payloads, table data, and IAM/resource policy JSON during mapping.
- Set `MaxRecords=100` and follow provisioned Redshift `Marker` pagination.
  Follow Serverless `NextToken` pagination.
- Record every AWS SDK call with bounded telemetry labels only. Use the
  `operation` attribute to distinguish provisioned from Serverless calls;
  do not widen the `service` label.
- Keep ARNs, endpoints, tags, KMS key IDs, parameter group names, namespace
  names, workgroup names, and raw AWS error payloads out of metric labels.

## Common Changes

- Add a new safe metadata field by updating `mapper.go` and writing an adapter
  test that proves master user names, admin user names, master password
  secret ARNs, master password KMS key IDs, and target-action JSON are still
  dropped.
- Add a new Redshift API call only after checking official AWS docs and
  updating the scanner package README with the metadata-only reason.
- Update `client.go` API-call telemetry whenever pagination or tag reads
  change.

## What Not To Change Without An ADR

- Do not add Redshift or Redshift Serverless mutation APIs, snapshot content
  reads, query APIs, data-plane calls, or graph writes.
- Do not move credential acquisition, STS, or target authorization into this
  package.
- Do not synthesize Redshift cluster ARNs from `ClusterNamespaceArn`; cluster
  ARNs must come from `(region, account_id, cluster_identifier)`.
