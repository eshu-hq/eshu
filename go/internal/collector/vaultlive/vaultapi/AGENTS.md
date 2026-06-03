# AGENTS.md — vaultapi

Scoped agent instructions for the net/http Vault metadata adapter (#1356).

## Mandatory

- **Metadata-only.** Never add a call to a KV `/data/` path or any endpoint that
  returns a secret value, token, or AppRole `secret_id`. The `isKVDataPath`
  guard in `doRequest` must stay; extend it rather than weaken it. Keep
  `TestIsKVDataPath` and `TestDataPathGuardRejectsBeforeRequest` passing.
- **No Vault SDK dependency.** Use the standard library (`net/http`,
  `encoding/json`). Do not add `hashicorp/vault/*` to go.mod here.
- Never store a raw ACL policy body — hash it (`hashPolicyBody`). Do not add
  rule-body text to the returned views.
- Bound any recursion or fan-out (see `kvMaxRecursion`, `kvMaxPathsScan`,
  `customMetaLimit`); a Vault tree is operator-controlled and may be hostile.
- This package implements the `vaultlive.Client` seam only. It performs no
  fingerprinting beyond the policy-body hash and no graph writes; redaction and
  emission are owned by `vaultlive` + `secretsiam`.
- Apply TDD with the `httptest` mock; add a mock response + assertion before a
  new endpoint mapping.

## Verification

- `go test ./internal/collector/vaultlive/vaultapi/...`
- `golangci-lint run ./internal/collector/vaultlive/vaultapi/...`
- `scripts/verify-package-docs.sh`
