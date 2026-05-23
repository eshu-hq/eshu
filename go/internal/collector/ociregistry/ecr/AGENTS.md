# ecr Agent Guidance

## Read First

1. `README.md` and `doc.go` for ECR boundaries.
2. `adapter.go` for registry URI and Distribution client helpers.
3. `auth.go` for the fakeable ECR token conversion seam.
4. `adapter_test.go`, `auth_test.go`, and `live_test.go` for fake and opt-in
   live coverage.
5. `../README.md` for OCI registry evidence boundaries.

## Local Rules

- Keep ECR support in `ociregistry`, not `packageregistry`.
- Do not commit AWS account IDs, repository names, credentials, profile-only
  runbooks, or private topology.
- Keep AWS profile, STS, account policy, and target-account decisions in
  runtime wiring.
- This package may convert `GetAuthorizationToken` output into Distribution
  credentials; decoded tokens must stay out of errors, logs, metrics, facts,
  docs, and PR text.
- Live tests must skip unless explicit environment variables opt in.

## Change Rules

- Add token behavior through fakeable interfaces before touching live tests.
- Do not use ECR image evidence as package ownership truth.
- Do not add package-feed behavior here; AWS package feeds are CodeArtifact.
