# AGENTS.md — collector-vault-live

Scoped instructions for the live Vault collector binary (#1356).

## Mandatory
- Metadata-only: never wire a code path that reads a Vault secret value. The
  client is `vaultapi` behind the `vaultlive.Client` seam, which rejects `/data/`.
- The read-only token must come from the environment (`token_env`), never from
  the serialized targets JSON. Do not log the token or the raw Vault address.
- Mirror `cmd/collector-kubernetes-live` for runtime structure (main/service/
  config) and the shared `collector.Service` commit boundary.
- Apply TDD for config parsing.

## Verification
- `go test ./cmd/collector-vault-live/...`
- `golangci-lint run ./cmd/collector-vault-live/...`
- `scripts/verify-package-docs.sh`
