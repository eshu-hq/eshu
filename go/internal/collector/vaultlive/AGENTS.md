# AGENTS.md — vaultlive

Scoped agent instructions for the live Vault source lane (issue #25, #1344).

## Mandatory

- This lane is **metadata-only**. Never add a `Client` method, mapping, or fact
  payload that reads or persists a secret value, Vault token, AppRole
  `secret_id`, OIDC client secret, private key, or bearer token. Never call a
  Vault KV `/data` endpoint.
- Keep `TestClientSurfaceIsMetadataOnly` passing. If you add a `Client` method,
  it must list or describe metadata only; extend the forbidden-substring guard
  rather than weaken it.
- Emit facts only through the `secretsiam` envelope builders so mount paths, key
  names, and accessors are fingerprinted. Do not construct `facts.Envelope`
  directly with raw Vault paths or names.
- Collectors observe; the reducer correlates. Do not add trust-chain logic,
  cross-source joins, or graph writes here.
- Apply TDD: add the failing test before the mapping for each new `vault_*` fact
  family.

## Patterns to follow

- Mirror `internal/collector/kuberneteslive` for the live-source / client-seam /
  claim-driven shape, and `internal/collector/awscloud/services/iam` for
  runtimebind registration when wiring the live adapter.
- Reuse `internal/collector/secretsiam` observation types and envelope builders;
  do not duplicate redaction logic.

## Verification

- `go test ./internal/collector/vaultlive/...`
- `golangci-lint run ./internal/collector/vaultlive/...`
- `scripts/verify-package-docs.sh`
