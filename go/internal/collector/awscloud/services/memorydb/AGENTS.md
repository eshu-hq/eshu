# AGENTS.md - internal/collector/awscloud/services/memorydb guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned MemoryDB domain types.
3. `scanner.go` - cluster, subnet group, parameter group, user, ACL, and
   snapshot fact emission.
4. `relationships.go` - cluster and ACL relationship emission.
5. `../../README.md` - shared AWS cloud observation and envelope contract.
6. `docs/public/services/collector-aws-cloud.md` - AWS collector service
   coverage and runtime requirements.

## Invariants

- Keep MemoryDB API access behind `Client`; do not import the AWS SDK into this
  package.
- Never call CreateCluster, DeleteCluster, UpdateCluster, CreateUser,
  DeleteUser, UpdateUser, CreateACL, DeleteACL, UpdateACL, CopySnapshot,
  DeleteSnapshot, or any mutation/data API.
- Never persist AUTH token values, user passwords, the raw user access string,
  cache keys, cache values, or snapshot data.
- `User` carries identity, status, authentication type, password count, and a
  non-secret `AccessStringPresent` signal only; the raw `AccessString` AWS
  field must be dropped at the adapter boundary and never reappear in scanner
  code.
- `ACL` carries identity, status, member user names, and associated cluster
  names only; do not add grant strings or password material.
- Snapshot facts carry name, source cluster, source, and status only; do not
  extend `SnapshotMetadata` with cluster configuration, shards, engine version,
  KMS key, snapshot window, snapshot retention, or AUTH token state.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, or deployable-unit truth from cluster names, user names, ACL
  names, or tags.
- Preserve stable identities (ARN preferred, then cluster/group/user/ACL names)
  across repeated observations in the same AWS generation.
- Keep cluster names, user names, ACL names, parameter group families, tags,
  and ARNs out of metric labels.

## Common Changes

- Add a new MemoryDB metadata field by extending the scanner-owned type,
  writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders. Reject additions that would expose AUTH tokens,
  passwords, access strings, or cache data.
- Add new relationship evidence only when the MemoryDB API reports both sides
  directly and the target identity is not sensitive.
- Extend SDK pagination, shard-detail replica derivation, or tag reads in the
  `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not create, delete, or modify clusters, users, ACLs, parameter groups, or
  snapshots.
- Do not resolve cluster names, user names, ACL names, or tags into workload
  ownership here; correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
- Do not widen the `User`, `ACL`, or `SnapshotMetadata` types beyond the fields
  listed in `README.md`.
