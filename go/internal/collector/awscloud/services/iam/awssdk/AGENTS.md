# AGENTS.md - internal/collector/awscloud/services/iam/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - IAM SDK pagination, trust policy decoding, and telemetry.
3. `../scanner.go` - scanner-owned IAM fact selection.
4. `../../README.md` - AWS cloud envelope contract.
5. `docs/docs/adrs/2026-04-20-aws-cloud-scanner-collector.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep IAM SDK calls here, not in `cmd/collector-aws-cloud` or the scanner
  package.
- Wrap each AWS paginator page in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Decode IAM trust policy JSON before returning scanner-owned role records.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new IAM API read by extending `iam.Client`, writing a scanner or adapter
  test first, then mapping the SDK response into scanner-owned types.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.
- Extend role mapping in `mapRole` when AWS source data needs to become
  scanner-owned evidence.

## What Not To Change Without An ADR

- Do not infer workload, environment, deployment, or ownership truth from IAM
  names, paths, tags, or policy text.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
