# AGENTS.md - internal/collector/awscloud/services/mgn/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - MGN SDK pagination, snapshot wiring, and telemetry.
3. `mapper.go` - safe SDK-to-scanner metadata mapping.
4. `exclusion_test.go` - the build-time gate that fails if a replication-secret
   read or mutation method reaches the adapter interface.
5. `../scanner.go` - scanner-owned MGN fact selection.
6. `../README.md` - MGN scanner contract.
7. `../../../README.md` - AWS cloud envelope contract.
8. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep MGN SDK calls here, not in `cmd/collector-aws-cloud` or the scanner
  package.
- Keep the `apiClient` interface limited to `Describe*`/`List*`/`Get*`
  control-plane reads. The exclusion test fails the build if any method is not a
  control-plane read or matches a replication-secret/mutation name; do not
  loosen it.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and result.
- Persist only safe migration metadata. Never read or persist replication-agent
  credentials, replication configuration secrets, replicated disk contents, or
  the `DataReplicationInfo.ReplicatorId`.
- Treat a missing launch configuration (not-found) as absent metadata, not a
  scan failure. Do not retry inside the adapter.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new MGN metadata read by extending `Client` and the `apiClient`
  interface with another `Describe*`/`List*`/`Get*` read, writing a scanner or
  adapter test first, then mapping the SDK response into scanner-owned types via
  `mapper.go`. The exclusion test rejects any mutation or replication-secret
  addition.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.
- Extend resource mapping only for AWS source data that is metadata and does not
  reveal replication-agent credentials, replication secrets, or replicated disk
  contents.

## What Not To Change Without An ADR

- Do not call `GetReplicationConfiguration`, the replication-configuration-
  template reads, or any MGN mutation or replication-control API.
- Do not map `DataReplicationInfo.ReplicatorId`, replicated disk detail, or any
  staging-area credential field.
- Do not infer workload, environment, deployment, or ownership truth from MGN
  names or tags.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
