# Production validation: secrets/IAM access paths

Validation-Slug: prod-secrets-iam-secret-access-paths
Validation-Tier: deployed_services
Validation-Date: 2026-08-08
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5681-claim-honesty-20260808-7 ESHU_POSTGRES_PORT=36542 NEO4J_BOLT_PORT=36687 NEO4J_HTTP_PORT=36474 GATE_API_PORT=36080 GATE_MCP_PORT=36091 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh >/tmp/eshu-5681-b7-postrebase2.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: secrets_iam.secret_access_paths.list returned one exact deployed MCP access path with a synthetic KV-path fingerprint and the read capability, without exposing a secret value.
B12-Assertion: secrets_iam.secret_access_paths.list -> mcp:list_secrets_iam_secret_access_paths

## Observed result

The fresh Compose run rebuilt the binaries, replayed the credential-free
Kubernetes, AWS, and Vault cassettes, drained all work, and queried
`list_secrets_iam_secret_access_paths` through MCP. The response contained one
row and matched the exact-state, KV-fingerprint, and `read` capability pins in
`testdata/golden/e2e-20repo-snapshot.json`. The complete gate finished with 532
passes, zero required failures, and zero advisory warnings.

The synthetic policy and metadata inputs are committed in
`testdata/cassettes/vaultlive/supply-chain-demo.json`.
