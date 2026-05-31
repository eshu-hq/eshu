# AGENTS.md - internal/collector/awscloud/services/networkmanager/awssdk guidance

## Read First

1. `README.md` - package purpose, global-service region pinning, telemetry, and
   invariants.
2. `client.go` - the read interface, region pinning, top-level Snapshot, and
   telemetry wrapper.
3. `mappers.go` - per-API pagination loops and the core-network resolve.
4. `maptypes.go` - SDK-type to scanner-type mapping.
5. `exclusion_test.go` - the build-time gate that fails if a mutation method
   reaches the adapter interface, plus the partition-aware region test.
6. `../scanner.go` - scanner-owned Network Manager fact selection.
7. `../README.md` - Network Manager scanner contract.
8. `../../../README.md` - AWS cloud envelope contract.

## Invariants

- Keep Network Manager SDK calls here, not in `cmd/collector-aws-cloud` or the
  scanner package.
- Network Manager is global: pin the partition's control-plane region in
  `NewClient` via `globalServiceRegion(awscloud.PartitionForBoundary(...))`.
  Never hardcode a single region; GovCloud and China use different endpoints.
- Keep the `apiClient` interface limited to `Describe`/`Get`/`List` reads. The
  exclusion test fails the build if any method matches a mutation prefix or the
  RouteAnalysis substring; do not loosen it.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, and operation.
- Persist only safe control-plane metadata plus resource tags. Never read route
  analyses, network routes, network telemetry, or routing policy documents.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new Network Manager metadata read by extending `Client` and the
  `apiClient` interface with another `Describe`/`Get`/`List` read, writing a
  scanner or adapter test first, then mapping the SDK response into
  scanner-owned types. The exclusion test rejects any mutation addition.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.

## What Not To Change Without An ADR

- Do not read route analyses, routes, telemetry, or routing policy, or call any
  Network Manager mutation API.
- Do not unpin or hardcode the control-plane region; keep it partition-derived.
- Do not infer workload, environment, deployment, or ownership truth from
  Network Manager names, locations, or tags.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
