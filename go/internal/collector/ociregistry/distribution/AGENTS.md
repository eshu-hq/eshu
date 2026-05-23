# AGENTS.md — internal/collector/ociregistry/distribution guidance

## Read First

1. `README.md` — package purpose and wire-contract boundaries
2. `client.go` — bounded OCI Distribution HTTP calls
3. `client_test.go` — fake-server contract coverage
4. `docs/docs/adrs/2026-05-10-oci-container-registry-collector.md`

## Invariants

- Keep this package provider-neutral. No ECR, JFrog, Docker Hub, GHCR, ACR, GAR,
  Harbor, or Quay discovery logic belongs here.
- Treat `401` auth challenge from `/v2/` as a valid endpoint signal, not as
  collection failure.
- Do not put tokens, repository names, tags, digests, or private paths in
  metric labels.
- Do not parse SBOMs, signatures, attestations, or vulnerability payloads here.

## Common Changes

- Add OCI endpoint support only when the Distribution spec or a documented
  compatible extension defines the route.
- Add provider quirks in provider packages, then keep the translated call shape
  narrow before it reaches `Client`.

## What Not To Change Without An ADR

- Do not turn this package into a graph projector or reducer.
- Do not decide that missing Referrers API means a digest has no referrers.
- Do not add package-manager registry behavior here.
