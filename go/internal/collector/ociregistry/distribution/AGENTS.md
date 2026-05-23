# distribution Agent Guidance

## Read First

1. `README.md` and `doc.go` for provider-neutral wire boundaries.
2. `client.go` for bounded OCI Distribution HTTP calls.
3. `token.go` for bearer-token request behavior.
4. `client_test.go` and `token_test.go` for fake-server contract coverage.
5. `../README.md` for OCI registry evidence boundaries.

## Local Rules

- Keep this package provider-neutral. No ECR, JFrog, Docker Hub, GHCR, ACR,
  GAR, Harbor, Quay, registry discovery, graph, reducer, or package-manager
  behavior belongs here.
- Treat a `401` challenge from `/v2/` as a valid endpoint signal when the
  Distribution headers support it.
- Keep credentials in request headers only. Do not put tokens, repository
  names, tags, digests, private paths, or private hosts in metric labels,
  errors, facts, docs, or PR text.
- Preserve slash-aware repository path escaping and `/v2/` endpoint handling.
- Do not parse SBOMs, signatures, attestations, vulnerability payloads, or
  package-manager metadata here.

## Change Rules

- Add OCI endpoint support only when the Distribution spec or a documented
  compatible extension defines the route.
- Add provider quirks in provider packages, then translate them into the
  narrow provider-neutral call shape before they reach `Client`.
- Do not decide that missing Referrers API support means a digest has no
  referrers; callers must emit warning evidence.
