# AGENTS.md - internal/collector/awscloud/services/location/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - the metadata-only `apiClient` interface, `Client`, `NewClient`,
   `Snapshot` orchestration, and telemetry.
3. `mappers.go` - per-family list pagination, describe reads, and safe metadata
   mapping.
4. `exclusion_test.go` - the build-time gate that fails if a data-plane read,
   key read, or mutation method reaches the adapter interface.
5. `../scanner.go` - scanner-owned Location Service fact selection.
6. `../README.md` - Location Service scanner contract.
7. `../../../README.md` - AWS cloud envelope contract.
8. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Location Service SDK calls here, not in `cmd/collector-aws-cloud` or the
  scanner package.
- Keep the `apiClient` interface limited to `List*` and `Describe*` reads. The
  exclusion test fails the build if any method is not a `List`/`Describe` read or
  matches a data-plane / key / mutation name; do not loosen it.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and result.
- Persist only safe resource metadata and tracker consumer ARNs. Never read or
  persist device positions, geofence geometries, place results, route
  calculations, map tiles, or API key material.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new Location Service metadata read by extending `Client` and the
  `apiClient` interface with another `List*`/`Describe*` read, writing a scanner
  or adapter test first, then mapping the SDK response into scanner-owned types.
  The exclusion test rejects any non-`List`/`Describe` addition.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.
- Extend resource mapping only for AWS source data that is metadata and does not
  reveal device-position, geofence-geometry, place-result, or route content.

## What Not To Change Without An ADR

- Do not read device positions, geofence geometries, place results, route
  calculations, or map tiles, or call any Location Service mutation API.
- Do not read API key material (`ListKeys`/`DescribeKey`).
- Do not infer workload, environment, deployment, or ownership truth from
  Location Service names or tags.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
