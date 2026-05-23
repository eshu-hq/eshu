# AGENTS.md — internal/collector/ociregistry/ghcr guidance

## Read First

1. `README.md` — GHCR boundary and exported helpers
2. `adapter.go` — repository validation and token-backed client
3. `live_test.go` — opt-in public GHCR validation gate
4. `docs/public/deployment/service-runtimes-collectors.md`

## Invariants

- GHCR support belongs in `ociregistry`, not `packageregistry`.
- Keep `ghcr.io` as both the canonical identity host and wire endpoint.
- Do not commit GitHub usernames, tokens, private package names, or
  account-only runbooks.
- Live tests must skip unless explicit environment variables opt in.

## Common Changes

- Add GHCR auth behavior through fakeable token/client helpers before touching
  live tests.
- Keep decoded tokens out of errors, logs, metrics, and docs.

## What Not To Change Without An ADR

- Do not infer GitHub source ownership from GHCR metadata without reducer-side
  correlation evidence.
- Do not treat tags as immutable image truth.
