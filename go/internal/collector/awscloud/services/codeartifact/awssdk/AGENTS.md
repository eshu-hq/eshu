# AGENTS.md - internal/collector/awscloud/services/codeartifact/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - CodeArtifact SDK pagination, mapping, and telemetry.
3. `../scanner.go` - scanner-owned CodeArtifact fact selection.
4. `../README.md` - CodeArtifact scanner contract.
5. `../../../README.md` - AWS cloud envelope contract.
6. `docs/public/services/collector-aws-cloud.md` - AWS collector service
   coverage and runtime requirements.

## Invariants

- Keep CodeArtifact SDK calls here, not in `cmd/collector-aws-cloud` or the
  scanner package.
- Keep the `apiClient` interface limited to `List`/`Describe` reads. Never add a
  package-payload read (`GetPackageVersionAsset`, `GetPackageVersionReadme`,
  `ListPackages`, `ListPackageVersions`, `ListPackageVersionAssets`,
  `ListPackageVersionDependencies`) or any mutation. The reflection guard test
  enforces this; do not weaken it.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Pass API-reported ARNs (domain, repository, encryption key, S3 bucket)
  through unchanged so they stay partition-aware.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new CodeArtifact metadata read by extending `codeartifact.Client`,
  writing an adapter test first, then mapping the SDK response into
  scanner-owned types.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.

## What Not To Change Without An ADR

- Do not infer workload, environment, deployment, or ownership truth from
  CodeArtifact domain or repository names.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
