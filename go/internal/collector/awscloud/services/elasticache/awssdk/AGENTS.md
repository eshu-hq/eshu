# AGENTS.md - internal/collector/awscloud/services/elasticache/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - ElastiCache SDK pagination, KMS and subnet resolution
   caching, and telemetry.
3. `mapper.go` - safe mapping of AWS SDK responses to scanner-owned types.
4. `../scanner.go` - scanner-owned ElastiCache fact selection.
5. `../README.md` - ElastiCache scanner contract.
6. `../../../README.md` - AWS cloud envelope contract.
7. `docs/public/services/collector-aws-cloud.md` - AWS collector service
   coverage and runtime requirements.

## Invariants

- Keep ElastiCache SDK calls here, not in `cmd/collector-aws-cloud` or the
  scanner package.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Persist only safe cluster, replication group, parameter group, subnet
  group, user, user group, snapshot, and tag metadata.
- Drop `User.Passwords` and `User.AccessString` before scanner code sees them.
  Re-introducing either is a security regression.
- Persist snapshot name, source identity, and status only; node snapshot
  detail, engine, engine version, KMS keys, snapshot windows, snapshot
  retention, and AUTH token state are intentionally not projected.
- Resolve cache cluster KMS from the replication group's `KmsKeyId` and
  cache cluster VPC/subnet from the cache subnet group; use the per-client
  cache helpers so a single AWS pagination cycle serves both the resolution
  pass and the scanner's group passes.
- Do not call CreateCacheCluster, DeleteCacheCluster, ModifyCacheCluster,
  CreateReplicationGroup, DeleteReplicationGroup, ModifyReplicationGroup,
  CreateUser, DeleteUser, ModifyUser, ModifyUserGroup, CopySnapshot,
  DeleteSnapshot, ExportSnapshotsToS3, or any other mutation or data API.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new ElastiCache metadata read by extending `Client`, writing a
  scanner or adapter test first, then mapping the SDK response into
  scanner-owned types.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy
  evidence shows the code is retry/throttle-shaped.
- Extend mapping only for AWS source data that is metadata and does not
  reveal AUTH tokens, password material, ACL grant strings, cache data, or
  snapshot payloads.

## What Not To Change Without An ADR

- Do not call cache data, AUTH token rotation, snapshot copy/export, or
  mutation APIs.
- Do not infer workload, environment, deployment, or ownership truth from
  cluster names, replication group IDs, user names, parameter group
  families, or tags.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
- Do not project `User.Passwords`, `User.AccessString`, snapshot node
  details, snapshot engine, snapshot engine version, snapshot KMS keys, or
  AUTH token state into scanner-owned types.
