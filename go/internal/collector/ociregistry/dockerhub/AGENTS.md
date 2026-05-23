# dockerhub Agent Guidance

## Read First

1. `README.md` and `doc.go` for Docker Hub boundaries.
2. `adapter.go` for repository normalization and token-backed client wiring.
3. `adapter_test.go` and `live_test.go` for fake and opt-in live coverage.
4. `../README.md` for OCI registry evidence boundaries.

## Local Rules

- Keep Docker Hub support in `ociregistry`, not `packageregistry`.
- Keep `docker.io` as the canonical identity host and
  `registry-1.docker.io` as the wire endpoint.
- Do not commit Docker Hub usernames, passwords, tokens, private repositories,
  account-only runbooks, or private topology.
- Keep decoded tokens out of errors, logs, metrics, facts, docs, and PR text.
- Live tests must skip unless explicit environment variables opt in.

## Change Rules

- Add auth, token, or rate-limit behavior through fakeable helpers before
  touching live tests.
- Do not treat Docker Hub tags as immutable image truth or add package-feed
  behavior here.
