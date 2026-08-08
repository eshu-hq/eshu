# Production validation: secrets/IAM posture gaps

Validation-Slug: prod-secrets-iam-posture-gaps
Validation-Tier: deployed_services
Validation-Date: 2026-08-08
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5681-claim-honesty-20260808-5 ESHU_POSTGRES_PORT=31542 NEO4J_BOLT_PORT=31687 NEO4J_HTTP_PORT=31474 GATE_API_PORT=31080 GATE_MCP_PORT=31091 bash scripts/verify-golden-corpus-gate.sh >/tmp/eshu-5681-b7-final.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: secrets_iam.posture_gaps.list returned one deployed MCP posture gap and preserved its unsupported-policy-layer type and unsupported state.

## Observed result

The fresh Compose run rebuilt the binaries, replayed the credential-free
cassettes, drained all work, and queried `list_secrets_iam_posture_gaps`
through MCP. The response contained one row and matched
`gap_type=unsupported_policy_layer` and `state=unsupported` in
`testdata/golden/e2e-20repo-snapshot.json`. The complete gate finished with 532
passes, zero required failures, and zero advisory warnings.

The public-safe source warning is committed in
`testdata/cassettes/awscloud/supply-chain-demo.json`; the reducer intentionally
normalizes that warning into the read-surface gap type asserted here.
