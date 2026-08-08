# Production validation: secrets/IAM privilege posture

Validation-Slug: prod-secrets-iam-privilege-posture-observations
Validation-Tier: deployed_services
Validation-Date: 2026-08-08
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5681-claim-honesty-20260808-6 ESHU_POSTGRES_PORT=34542 NEO4J_BOLT_PORT=34687 NEO4J_HTTP_PORT=34474 GATE_API_PORT=34080 GATE_MCP_PORT=34091 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh >/tmp/eshu-5681-b7-postrebase.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: secrets_iam.privilege_posture_observations.list returned one bounded wildcard-trust observation through the deployed MCP surface with high severity and partial state.

## Observed result

The fresh Compose run rebuilt the binaries, replayed the credential-free
cassettes, drained all work, and queried
`list_secrets_iam_privilege_posture_observations` through MCP. The response
contained one row and matched the pinned `wildcard_trust`, `high`, and `partial`
values in `testdata/golden/e2e-20repo-snapshot.json`. The complete gate finished
with 532 passes, zero required failures, and zero advisory warnings.

The observed row comes from public-safe synthetic trust-policy evidence in
`testdata/cassettes/awscloud/supply-chain-demo.json`.
