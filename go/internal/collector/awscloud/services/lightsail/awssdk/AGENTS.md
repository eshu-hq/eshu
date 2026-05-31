# AGENTS.md - internal/collector/awscloud/services/lightsail/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - Lightsail SDK pagination and telemetry.
3. `mapper.go` - SDK-to-scanner safe metadata mapping.
4. `exclusion_test.go` - the metadata-only reflective guard on `apiClient`.
5. `../scanner.go` - scanner-owned Lightsail fact selection.
6. `../README.md` - Lightsail scanner contract.
7. `../../../README.md` - AWS cloud envelope contract.
8. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Lightsail SDK calls here, not in `cmd/collector-aws-cloud` or the
  scanner package.
- Keep the `apiClient` interface limited to the five bounded Get* readers.
  Adding any mutation, lifecycle, attach/detach, or secret-read method breaks
  `exclusion_test.go` by design.
- Wrap each AWS paginator page in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Persist only safe instance, database, load balancer, disk, and static IP
  metadata. Never propagate database master passwords, instance access keys,
  or default key-pair private material.
- Forward Lightsail resource ARNs exactly as AWS reports them. Never
  synthesize an ARN.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new Lightsail metadata read by extending `apiClient` and `Client` with
  another bounded Get* reader, writing a scanner or adapter test first, then
  mapping the SDK response into scanner-owned types. Update
  `exclusion_test.go`'s expected method set in the same change.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy
  evidence shows the code is retry/throttle-shaped.
- Extend resource mapping only for AWS source data that is metadata and does
  not reveal credentials, access secrets, or payload content.

## What Not To Change Without An ADR

- Do not add any Lightsail mutation, lifecycle, attach/detach, or secret-read
  API to `apiClient`.
- Do not call `GetInstanceAccessDetails`, `DownloadDefaultKeyPair`,
  `GetRelationalDatabaseMasterUserPassword`, or any other credential reader.
- Do not infer workload, environment, deployment, or ownership truth from
  Lightsail names or tags.
- Do not write facts, graph rows, or reducer-owned state here.
