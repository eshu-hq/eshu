# AGENTS.md - internal/collector/awscloud/services/elasticache guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned ElastiCache domain types.
3. `scanner.go` - cache cluster, replication group, parameter group, subnet
   group, user, user group, and snapshot fact emission.
4. `relationships.go` - cluster, replication group, and user group
   relationship emission.
5. `../../README.md` - shared AWS cloud observation and envelope contract.
6. `docs/public/services/collector-aws-cloud.md` - AWS collector service
   coverage and runtime requirements.

## Invariants

- Keep ElastiCache API access behind `Client`; do not import the AWS SDK into
  this package.
- Never call CreateCacheCluster, DeleteCacheCluster, ModifyCacheCluster,
  CreateReplicationGroup, DeleteReplicationGroup, ModifyReplicationGroup,
  CreateUser, DeleteUser, ModifyUser, or any mutation/data API.
- Never persist AUTH token values, user passwords, user access strings, cache
  keys, cache values, or snapshot data.
- `User` and `UserGroup` carry identity, status, engine, and authentication
  type only; the `Passwords` and `AccessString` AWS fields must be dropped at
  the adapter boundary and never reappear in scanner code.
- Snapshot facts carry name, source identity, and status only per #713; do not
  extend `SnapshotMetadata` with engine, engine version, KMS key, snapshot
  window, snapshot retention, node-snapshot detail, or AUTH token state.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, or deployable-unit truth from cluster names, replication group
  IDs, user names, or tags.
- Preserve stable identities (ARN preferred, then cluster/group/user IDs)
  across repeated observations in the same AWS generation.
- Keep cache cluster IDs, replication group IDs, parameter group families,
  user IDs, tags, and ARNs out of metric labels.

## Common Changes

- Add a new ElastiCache metadata field by extending the scanner-owned type,
  writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders. Reject additions that would expose AUTH
  tokens, passwords, access strings, or cache data.
- Add new relationship evidence only when the ElastiCache API reports both
  sides directly and the target identity is not sensitive.
- Extend SDK pagination, response caching, or KMS/subnet resolution in the
  `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not create, delete, or modify cache clusters, replication groups, users,
  user groups, parameter groups, or snapshots.
- Do not resolve cluster names, replication group IDs, user names, or tags
  into workload ownership here; correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
- Do not widen the `User` or `SnapshotMetadata` types beyond the fields listed
  in `README.md`.
