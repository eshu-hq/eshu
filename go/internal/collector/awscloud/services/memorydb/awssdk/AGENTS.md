# AGENTS.md - internal/collector/awscloud/services/memorydb/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - MemoryDB SDK pagination, tag reads, and telemetry.
3. `mapper.go` - safe mapping of AWS SDK responses to scanner-owned types,
   including per-shard replica derivation.
4. `../scanner.go` - scanner-owned MemoryDB fact selection.
5. `../README.md` - MemoryDB scanner contract.
6. `../../../README.md` - AWS cloud envelope contract.
7. `docs/public/services/collector-aws-cloud.md` - AWS collector service
   coverage and runtime requirements.

## Invariants

- Keep MemoryDB SDK calls here, not in `cmd/collector-aws-cloud` or the scanner
  package.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Persist only safe cluster, subnet group, parameter group, user, ACL,
  snapshot, and tag metadata.
- Drop `User.AccessString` before scanner code sees it and record only
  `AccessStringPresent`. Re-introducing the raw access string or any password
  material is a security regression.
- Persist snapshot name, source cluster identity, source, and status only;
  cluster configuration, shards, engine version, KMS keys, snapshot windows,
  snapshot retention, and AUTH token state are intentionally not projected.
- Derive the per-shard replica count from the shard node counts returned by
  `DescribeClusters` with `ShowShardDetails=true`; do not invent a replica
  count when AWS reports no shard detail.
- Do not call CreateCluster, DeleteCluster, UpdateCluster, CreateUser,
  DeleteUser, UpdateUser, CreateACL, DeleteACL, UpdateACL, CopySnapshot,
  DeleteSnapshot, or any other mutation or data API.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new MemoryDB metadata read by extending `Client`, writing a scanner or
  adapter test first, then mapping the SDK response into scanner-owned types.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy
  evidence shows the code is retry/throttle-shaped.
- Extend mapping only for AWS source data that is metadata and does not reveal
  AUTH tokens, password material, ACL grant strings, cache data, or snapshot
  payloads.

## What Not To Change Without An ADR

- Do not call cache data, AUTH token rotation, snapshot copy, or mutation APIs.
- Do not infer workload, environment, deployment, or ownership truth from
  cluster names, user names, ACL names, parameter group families, or tags.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
- Do not project `User.AccessString`, password material, snapshot cluster
  configuration, snapshot shards, snapshot engine version, snapshot KMS keys,
  or AUTH token state into scanner-owned types.
