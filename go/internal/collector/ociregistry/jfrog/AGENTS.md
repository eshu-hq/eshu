# AGENTS.md — internal/collector/ociregistry/jfrog guidance

## Read First

1. `README.md` — package purpose and JFrog boundary
2. `adapter.go` — Artifactory URL and credential mapping
3. `live_test.go` — opt-in private JFrog validation gate
4. `docs/docs/adrs/2026-05-10-oci-container-registry-collector.md`

## Invariants

- Keep JFrog Docker/OCI repository support separate from JFrog package feeds.
- Do not commit private hostnames, repository keys, user names, tokens, or image
  repository names.
- Live tests must skip unless explicit environment variables opt in.
- Provider code prepares calls; Distribution wire behavior belongs to the
  `distribution` package.

## Common Changes

- Add Artifactory-specific repository topology mapping here.
- Add package-feed behavior under `packageregistry`, not here.

## What Not To Change Without An ADR

- Do not make JFrog metadata canonical workload or package ownership truth.
- Do not flatten local, remote, and virtual repository topology into one
  unlabelled registry.
