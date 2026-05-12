# AGENTS.md — internal/collector/ociregistry/ecr guidance

## Read First

1. `README.md` — ECR boundary and exported helpers
2. `adapter.go` — registry URI and Distribution client helpers
3. `auth.go` — fakeable ECR token conversion seam
4. `live_test.go` — opt-in private ECR validation gate
5. `docs/docs/adrs/2026-05-10-oci-container-registry-collector.md`

## Invariants

- ECR support belongs in `ociregistry`, not `packageregistry`.
- Do not commit AWS account IDs, repository names, credentials, or profile-only
  runbooks.
- Keep AWS profile, STS, and account policy decisions in runtime wiring. This
  package may convert `GetAuthorizationToken` output into Distribution
  credentials.
- Live tests must skip unless explicit environment variables opt in.

## Common Changes

- Add ECR token behavior through fakeable interfaces before touching live tests.
- Keep decoded ECR tokens out of errors, logs, metrics, and docs.

## What Not To Change Without An ADR

- Do not use ECR image evidence as package ownership truth.
- Do not add package feed behavior here; AWS package feeds are CodeArtifact.
