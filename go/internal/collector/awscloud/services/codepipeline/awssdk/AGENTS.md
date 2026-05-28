# AGENTS.md - internal/collector/awscloud/services/codepipeline/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - CodePipeline SDK pagination, point reads, and telemetry.
3. `mapping.go` - SDK-to-scanner mapping, config-value dropping, target
   allowlist, and redaction.
4. `client_test.go` - the reflection guard and config-value/secret-token
   exclusion proofs.
5. `../scanner.go` - scanner-owned CodePipeline fact selection.
6. `../README.md` - CodePipeline scanner contract.
7. `../../../README.md` - AWS cloud envelope contract.
8. `docs/public/services/collector-aws-cloud.md` - AWS collector service
   coverage and runtime requirements.

## Invariants

- Keep CodePipeline SDK calls here, not in `cmd/collector-aws-cloud` or the
  scanner package.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Keep `apiClient` metadata-only. Do not add any mutation, execution-control,
  webhook-management, custom-action-mutation, or job-worker method; the
  reflection guard test enforces this. The job-worker plane returns secret
  configuration values.
- Never copy an action configuration value into a scanner type. `mapAction`
  keeps configuration KEY names only and reads target identifiers from the
  `targetConfigKeys` allowlist alone.
- Never read `WebhookAuthConfiguration.SecretToken` or a GitHub OAuthToken.
- Route source-revision summaries through `awscloud.RedactString`.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new CodePipeline metadata read by extending `codepipeline.Client`,
  writing a scanner or adapter test first, then mapping the SDK response into
  scanner-owned types.
- Add a new target provider by extending `targetProviderForAction` and the
  `targetConfigKeys` allowlist. The added configuration key must be a resource
  identifier, never a token; add a value-absence assertion when in doubt.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.

## What Not To Change Without An ADR

- Do not call mutation, execution-control, webhook-management, custom-action
  mutation, or job-worker APIs.
- Do not persist action configuration values, webhook secret tokens, or GitHub
  OAuth tokens.
- Do not infer workload, environment, deployment, or ownership truth from
  CodePipeline names, tags, or target links.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
