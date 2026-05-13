# AGENTS.md - internal/collector/awscloud/services/ecr/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - ECR SDK pagination, mapping, and telemetry.
3. `../scanner.go` - scanner-owned ECR fact selection.
4. `../README.md` - ECR scanner contract.
5. `../../../README.md` - AWS cloud envelope contract.
6. `docs/docs/adrs/2026-04-20-aws-cloud-scanner-collector.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep ECR SDK calls here, not in `cmd/collector-aws-cloud` or the scanner
  package.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Treat missing lifecycle policies as empty results, not scan failures.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new ECR API read by extending `ecr.Client`, writing a scanner or adapter
  test first, then mapping the SDK response into scanner-owned types.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.
- Extend image mapping in `mapImageDetail` when AWS source data needs to become
  scanner-owned evidence.

## What Not To Change Without An ADR

- Do not infer workload, environment, deployment, or ownership truth from ECR
  repository names, tags, or image digests.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
