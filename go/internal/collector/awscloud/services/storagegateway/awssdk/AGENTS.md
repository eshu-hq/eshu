# AGENTS.md - internal/collector/awscloud/services/storagegateway/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - Storage Gateway SDK pagination, gateway enrichment, and
   telemetry.
3. `map.go` - safe SDK-to-scanner type mapping.
4. `../scanner.go` - scanner-owned Storage Gateway fact selection.
5. `../README.md` - Storage Gateway scanner contract.
6. `../../../README.md` - AWS cloud envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Storage Gateway SDK calls here, not in `cmd/collector-aws-cloud` or the
  scanner package.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Persist only safe gateway, volume, and file-share metadata. Reduce gateway
  network interfaces to a count; never propagate raw IP addresses.
- Never propagate NFS client allow lists, SMB admin/user lists, local-console
  passwords, SMB guest passwords, or object contents.
- Keep the `apiClient` interface to list/describe operations only. The
  exclusion-reflection test fails on any mutation, cache, tape, or credential
  method name.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new Storage Gateway metadata read by extending `Client`, writing a
  scanner or adapter test first, then mapping the SDK response into
  scanner-owned types in `map.go`.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.
- Extend resource mapping only for AWS source data that is metadata and does not
  reveal object contents, client identity lists, or credentials.

## What Not To Change Without An ADR

- Do not call any gateway lifecycle, cache-refresh, volume/share mutation, tape,
  or credential API.
- Do not add a method to `apiClient` whose name carries a mutation, cache, tape,
  or credential fragment.
- Do not infer workload, environment, deployment, or ownership truth from
  Storage Gateway names or tags.
- Do not write facts, graph rows, or reducer-owned state here.
