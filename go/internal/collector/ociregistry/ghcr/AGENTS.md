# ghcr Agent Guidance

## Read First

1. `README.md` and `doc.go` for GHCR boundaries.
2. `adapter.go` for repository validation and token-backed client wiring.
3. `adapter_test.go` and `live_test.go` for fake and opt-in live coverage.
4. `../README.md` for OCI registry evidence boundaries.

## Local Rules

- Keep GHCR support in `ociregistry`, not `packageregistry`.
- Keep `ghcr.io` as both the canonical identity host and wire endpoint.
- Do not commit GitHub usernames, tokens, private package names, account-only
  runbooks, or private topology.
- Keep decoded tokens out of errors, logs, metrics, facts, docs, and PR text.
- Live tests must skip unless explicit environment variables opt in.

## Change Rules

- Add auth behavior through fakeable token/client helpers before touching live
  tests.
- Do not infer GitHub source ownership from GHCR metadata without reducer-side
  correlation evidence.
- Do not treat tags as immutable image truth.
