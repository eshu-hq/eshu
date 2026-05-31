# AGENTS.md - internal/collector/awscloud/services/vpclattice/awssdk guidance

## Read First

1. `README.md` - package purpose, read surface, telemetry, and invariants.
2. `client.go` - VPC Lattice SDK interface, Snapshot orchestration, tag reads,
   and telemetry.
3. `mappers.go` - service network, service, and listener pagination and mapping.
4. `target_mappers.go` - target group, target, and detail-read mapping.
5. `exclusion_test.go` - the build-time gate that fails if a policy-read or
   mutation method reaches the adapter interface.
6. `../scanner.go` - scanner-owned VPC Lattice fact selection.
7. `../README.md` - VPC Lattice scanner contract.
8. `../../../README.md` - AWS cloud envelope contract.
9. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep VPC Lattice SDK calls here, not in `cmd/collector-aws-cloud` or the
  scanner package.
- Keep the `apiClient` interface limited to `List*` and read-only `Get*`
  detail reads. The exclusion test fails the build if any method is not a `List`
  or `Get` read or matches a policy/mutation name; do not loosen it.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Persist only safe service network, service, target group, and listener
  metadata plus association evidence and resource tags. Never read or persist an
  auth-policy body, a resource-policy body, or any data-plane payload.
- Do not retry inside the adapter; the SDK's own retryer and the shared throttle
  classifier own retry/throttle accounting.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new VPC Lattice metadata read by extending `Client` and the `apiClient`
  interface with another `List*` or read-only `Get*` detail read, writing a
  scanner or adapter test first, then mapping the SDK response into
  scanner-owned types. The exclusion test rejects any policy-read or mutation
  addition.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.
- Extend resource mapping only for AWS source data that is metadata and does not
  reveal a policy body or any data-plane payload.

## What Not To Change Without An ADR

- Do not read auth-policy or resource-policy bodies, or call any VPC Lattice
  mutation API.
- Do not infer workload, environment, deployment, or ownership truth from VPC
  Lattice names or tags.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
