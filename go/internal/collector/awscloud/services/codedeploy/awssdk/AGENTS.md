# AGENTS.md - internal/collector/awscloud/services/codedeploy/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - CodeDeploy SDK pagination, batch resolution, and telemetry.
3. `mapping.go` - SDK-to-scanner mapping and redaction.
4. `client_test.go` - the reflection guard and appspec-exclusion proofs.
5. `../scanner.go` - scanner-owned CodeDeploy fact selection.
6. `../README.md` - CodeDeploy scanner contract.
7. `../../../README.md` - AWS cloud envelope contract.
8. `docs/public/services/collector-aws-cloud.md` - AWS collector service
   coverage and runtime requirements.

## Invariants

- Keep CodeDeploy SDK calls here, not in `cmd/collector-aws-cloud` or the
  scanner package.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Keep `apiClient` metadata-only. Do not add any mutation, deployment/target
  instance data-plane, or revision-body method; the reflection guard test
  enforces this.
- Never copy appspec.yml bodies (`AppSpecContent.Content`, `String_.Content`)
  into `RevisionSummary`.
- Route on-premises tag values through `awscloud.RedactString`. Do not persist
  raw on-premises tag values.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new CodeDeploy metadata read by extending `codedeploy.Client`, writing
  a scanner or adapter test first, then mapping the SDK response into
  scanner-owned types.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy
  evidence shows the code is retry/throttle-shaped.
- Extend mapping only for AWS source data that is metadata and reveals neither
  appspec bodies nor raw customer-PII tag values.

## What Not To Change Without An ADR

- Do not call mutation, deployment data-plane, instance data-plane, or
  revision-body APIs.
- Do not infer workload, environment, deployment, or ownership truth from
  CodeDeploy names, tags, or target links.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
