# AGENTS.md — internal/collector/ociregistry/dockerhub guidance

## Read First

1. `README.md` — Docker Hub boundary and exported helpers
2. `adapter.go` — repository normalization and token-backed client
3. `live_test.go` — opt-in public Docker Hub validation gate
4. `docs/docs/adrs/2026-05-10-oci-container-registry-collector.md`

## Invariants

- Docker Hub support belongs in `ociregistry`, not `packageregistry`.
- Keep `docker.io` as the canonical identity host and `registry-1.docker.io`
  as the wire endpoint.
- Do not commit Docker Hub usernames, passwords, tokens, private repositories,
  or account-only runbooks.
- Live tests must skip unless explicit environment variables opt in.

## Common Changes

- Add Docker Hub auth or rate-limit behavior through fakeable token/client
  helpers before touching live tests.
- Keep decoded tokens out of errors, logs, metrics, and docs.

## What Not To Change Without An ADR

- Do not treat Docker Hub tag evidence as immutable image truth.
- Do not add package-feed behavior here.
