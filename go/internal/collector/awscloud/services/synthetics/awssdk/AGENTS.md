# AGENTS.md - internal/collector/awscloud/services/synthetics/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - Synthetics SDK pagination, partition-aware ARN synthesis, safe
   metadata mapping, and telemetry.
3. `exclusion_test.go` - the build-time gate that fails if a run-read, code-read,
   or mutation method reaches the adapter interface.
4. `../scanner.go` - scanner-owned Synthetics fact selection.
5. `../README.md` - Synthetics scanner contract.
6. `../../../README.md` - AWS cloud envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Synthetics SDK calls here, not in `cmd/collector-aws-cloud` or the
  scanner package.
- Keep the `apiClient` interface limited to `DescribeCanaries`. The exclusion
  test fails the build if any method other than `DescribeCanaries` is present or
  if a run/code/mutation name matches; do not loosen it.
- Wrap each AWS paginator page in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Persist only safe canary metadata plus the inline resource tags. Never read or
  persist canary script source code, run artifacts, or run results.
- Synthesize the canary ARN with `awscloud.PartitionForBoundary`; never hardcode
  `arn:aws:`. Copy only the run-artifact S3 encryption configuration and the VPC
  config; never the artifacts themselves.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new Synthetics metadata field only from the `DescribeCanaries` response;
  write a scanner or adapter test first, then map the SDK field into
  scanner-owned types. Do not add a new SDK operation unless it is a pure
  control-plane metadata read with no run-artifact, run-result, or code content,
  and update the exclusion test accordingly.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.

## What Not To Change Without An ADR

- Do not read canary runs, run results, or canary code; do not call any
  Synthetics mutation or run-control API.
- Do not infer workload, environment, deployment, or ownership truth from
  Synthetics names or tags.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
