# AGENTS.md - internal/collector/awscloud/services/drs/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - DRS SDK pagination, safe metadata mapping, and telemetry.
3. `exclusion_test.go` - the build-time gate that fails if an agent-read,
   snapshot-read, job-log-read, or mutation method reaches the adapter
   interface.
4. `../scanner.go` - scanner-owned DRS fact selection.
5. `../README.md` - DRS scanner contract.
6. `../../../README.md` - AWS cloud envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep DRS SDK calls here, not in `cmd/collector-aws-cloud` or the scanner
  package.
- Keep the `apiClient` interface limited to `Describe*` reads. The exclusion
  test fails the build if any method is not a `Describe` read or matches an
  agent/snapshot/job-log/mutation name; do not loosen it.
- Wrap each AWS paginator page in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Persist only safe identity, replication state, origin, and configuration
  metadata plus resource tags. Never read or persist agent secrets, replicated
  disk data, snapshot contents, or job logs.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new DRS metadata read by extending `Client` and the `apiClient`
  interface with another `Describe*` read, writing a scanner or adapter test
  first, then mapping the SDK response into scanner-owned types. The exclusion
  test rejects any non-`Describe` addition.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.
- Extend resource mapping only for AWS source data that is metadata and does not
  reveal agent, replicated disk, or snapshot content.

## What Not To Change Without An ADR

- Do not read agent secrets, replicated disks, snapshots, or job logs, or call
  any DRS recover/start/stop/mutation API.
- Do not infer workload, environment, deployment, or ownership truth from DRS
  names or tags.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
