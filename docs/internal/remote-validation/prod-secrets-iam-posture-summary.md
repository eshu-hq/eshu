# Production validation: secrets/IAM posture summary

Validation-Slug: prod-secrets-iam-posture-summary
Validation-Tier: deployed_services
Validation-Date: 2026-08-08
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5681-claim-honesty-20260808-5 ESHU_POSTGRES_PORT=31542 NEO4J_BOLT_PORT=31687 NEO4J_HTTP_PORT=31474 GATE_API_PORT=31080 GATE_MCP_PORT=31091 bash scripts/verify-golden-corpus-gate.sh >/tmp/eshu-5681-b7-final.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: secrets_iam.posture_summary.read returned non-zero deployed MCP buckets for exact trust chains, wildcard privilege observations, exact access paths, and unsupported policy-layer gaps.

## Observed result

The fresh Compose run rebuilt the binaries, replayed the credential-free
cassettes, drained all work, and queried `count_secrets_iam_posture` through
MCP. All four capability-specific summary buckets were present with count one,
as pinned in `testdata/golden/e2e-20repo-snapshot.json`. The complete gate
finished with 532 passes, zero required failures, and zero advisory warnings.

This rollup is backed by the same committed synthetic Kubernetes, AWS, and
Vault inputs as the four list artifacts; it does not depend on the optional
secrets/IAM graph projection.
