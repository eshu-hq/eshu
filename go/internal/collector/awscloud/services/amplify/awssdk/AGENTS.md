# AGENTS.md - internal/collector/awscloud/services/amplify/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - Amplify SDK pagination, response mapping, and telemetry.
3. `exclusion_test.go` - the reflection guard proving the read surface is
   List-only and carries no mutation or token-read method.
4. `../scanner.go` - scanner-owned Amplify fact selection.
5. `../README.md` - Amplify scanner contract.
6. `../../../README.md` - AWS cloud envelope contract.
7. `docs/public/services/collector-aws-cloud.md` - AWS collector service
   coverage and runtime requirements.

## Invariants

- Keep Amplify SDK calls here, not in `cmd/collector-aws-cloud` or the scanner
  package.
- Wrap each AWS paginator page in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and result.
- Keep `apiClient` metadata-only. Do not add any mutation, lifecycle,
  payload-generating, or webhook method; the reflection guard test enforces a
  List-only surface.
- Never copy an app or branch environment-variable map, build-spec body, or
  basic-auth credential into a scanner type.
- Route every repository URL through `amplify.SanitizeRepositoryURL`.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new Amplify metadata read by extending `amplify.Client`, writing a
  scanner or adapter test first, then mapping the SDK response into
  scanner-owned types. Confirm the new field carries no env-var, build-spec, or
  credential material before mapping it.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.

## What Not To Change Without An ADR

- Do not call mutation, lifecycle, payload-generating, or webhook APIs.
- Do not persist environment variables, build-spec bodies, basic-auth
  credentials, or repository access tokens.
- Do not infer workload, environment, deployment, or ownership truth from
  Amplify names, tags, or domain links.
- Do not write facts, graph rows, or reducer-owned state here.
