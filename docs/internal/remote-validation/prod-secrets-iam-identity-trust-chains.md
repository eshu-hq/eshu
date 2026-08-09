# Production validation: secrets/IAM identity trust chains

Validation-Slug: prod-secrets-iam-identity-trust-chains
Validation-Tier: deployed_services
Validation-Date: 2026-08-08
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5681-claim-honesty-20260808-7 ESHU_POSTGRES_PORT=36542 NEO4J_BOLT_PORT=36687 NEO4J_HTTP_PORT=36474 GATE_API_PORT=36080 GATE_MCP_PORT=36091 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh >/tmp/eshu-5681-b7-postrebase2.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: secrets_iam.identity_trust_chains.list returned one exact trust chain through the deployed MCP surface, including the expected workload identity and state.
B12-Assertion: secrets_iam.identity_trust_chains.list -> mcp:list_secrets_iam_identity_trust_chains

## Observed result

The fresh Compose run rebuilt every host binary, replayed the credential-free
Kubernetes, AWS, and Vault cassettes, drained reducer and projector work to
terminal, and queried `list_secrets_iam_identity_trust_chains` through the MCP
server. The response contained one row and matched the non-vacuous
`identity_trust_chains[].workload_object_id` and `.state` assertions in
`testdata/golden/e2e-20repo-snapshot.json`. The complete gate finished with 532
passes, zero required failures, and zero advisory warnings.

The synthetic inputs are committed in
`testdata/cassettes/kuberneteslive/supply-chain-demo.json`,
`testdata/cassettes/awscloud/supply-chain-demo.json`, and
`testdata/cassettes/vaultlive/supply-chain-demo.json`.
