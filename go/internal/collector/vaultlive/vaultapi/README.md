# vaultapi

`vaultapi` is the `net/http` implementation of the `vaultlive.Client` seam — the
live Vault metadata client for the secrets/IAM posture collector (issue #25,
#1356). It uses only the Go standard library; there is **no Vault SDK
dependency**.

## Metadata-only by construction

- `doRequest` rejects any path containing a KV `/data/` segment (`isKVDataPath`)
  before issuing the request, so no code path can read a secret value.
- The adapter only ever calls metadata/list/describe endpoints: `sys/auth`,
  `sys/mounts`, `sys/policies/acl`, `auth/<mount>/role`, `identity/entity/id`,
  `identity/entity-alias/id`, and `<mount>/metadata` (KV v2).
- ACL policy bodies are hashed (`sha256:…`); the raw HCL never leaves the
  package. Rule parsing is intentionally deferred (the hash + name are the
  posture evidence).
- KV metadata is walked recursively from the metadata endpoint only, bounded by
  recursion depth and a total-paths cap so a deep or adversarial tree cannot run
  away. Custom-metadata key names are collected (capped); values are never read.

## Usage

```go
client, err := vaultapi.New(vaultapi.Config{
    Address: "https://vault.example.com:8200",
    Token:   readOnlyToken, // bound to the secrets/IAM read-only policy
})
// pass client to a vaultlive.Source as the injected vaultlive.Client
```

## Status

Adapter implementation + unit tests run against an `httptest` mock Vault.
`cmd/collector-vault-live` now wires this adapter into the claim-driven
`vault_live` runtime. Response shapes follow the documented Vault KV v2, auth,
policy, and identity APIs, while the package remains dependency-free per the
scoped no-SDK rule.

## Evidence

No-Regression Evidence: this adapter issues bounded, read-only Vault REST calls
over `net/http`; it performs no Cypher, no graph or canonical writes, holds no
locks/leases, and runs no queue. The one fan-out (the KV v2 metadata walk) is
bounded by `kvMaxRecursion` (depth) and `kvMaxPathsScan` (total paths) with an
8 MiB response-body limit per call, so a deep or hostile Vault tree cannot run
away. Correctness and the metadata-only/traversal guards are covered by
`go test ./internal/collector/vaultlive/vaultapi` (happy-path mapping for all
seven families, the no-`/data/` guarantee, traversal and query-injection
rejection of hostile LIST keys, and a secret legitimately named `data`). The
per-call latency/throughput profile against a live Vault is validated as part of
the #1356 integration step.

Observability Evidence: the adapter reports bounded API-call observations
through the `OnAPICall` hook, and the claimed Vault runtime records them as
`eshu_dp_secrets_iam_source_api_calls_total{source="vault",operation,result}`.
The adapter does not place paths, addresses, tokens, or response bodies in
metric labels.
