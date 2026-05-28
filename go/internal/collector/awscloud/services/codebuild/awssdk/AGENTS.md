# AGENTS.md - internal/collector/awscloud/services/codebuild/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - CodeBuild SDK pagination, batch resolution, and telemetry.
3. `mapping.go` - SDK-to-scanner mapping, redaction, and buildspec/log exclusion.
4. `client_test.go` - the reflection guard and the redaction/buildspec proofs.
5. `../scanner.go` - scanner-owned CodeBuild fact selection.
6. `../README.md` - CodeBuild scanner contract.
7. `../../../README.md` - AWS cloud envelope contract.
8. `docs/public/services/collector-aws-cloud.md` - AWS collector service
   coverage and runtime requirements.

## Invariants

- Keep CodeBuild SDK calls here, not in `cmd/collector-aws-cloud` or the scanner
  package.
- Wrap each AWS paginator page or batch read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Keep `apiClient` metadata-only. Do not add any mutation, build data-plane,
  source-credential, or log-content method; the reflection guard test enforces
  this.
- Never copy `ProjectSource.Buildspec` into scanner types.
- Never copy build log group/stream references or log content into `Build`.
- Route PLAINTEXT environment-variable values (and any unmapped variable type)
  through `awscloud.RedactString`. Keep PARAMETER_STORE/SECRETS_MANAGER values
  only as references.
- Chunk every BatchGet by the AWS 100-item cap and surface `*NotFound` lists as
  errors; never silently drop an unresolved item.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new CodeBuild metadata read by extending `codebuild.Client`, writing a
  scanner or adapter test first, then mapping the SDK response into
  scanner-owned types.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy
  evidence shows the code is retry/throttle-shaped.
- Extend mapping only for AWS source data that is metadata and reveals neither
  buildspec bodies, build logs, source credentials, nor raw PLAINTEXT values.

## What Not To Change Without An ADR

- Do not call mutation, build data-plane, source-credential, or log-content
  APIs.
- Do not infer workload, environment, deployment, or ownership truth from
  CodeBuild names, tags, or source links.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
