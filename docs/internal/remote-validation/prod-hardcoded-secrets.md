# prod-hardcoded-secrets — production validation

Validation-Slug: prod-hardcoded-secrets
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: security.hardcoded_secrets passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.
B12-Assertion: security.hardcoded_secrets -> mcp:investigate_hardcoded_secrets

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


Capability: `security.hardcoded_secrets` (tool
`investigate_hardcoded_secrets`). Production profile:
`required_runtime: deployed_services`, `max_scope_size: multi_repo_platform`,
`p95_latency_ms: 2500`, `max_truth_level: derived`.

## Claim validated

Bounded redacted secret-candidate investigation over the content index;
findings are redacted by default, with citations required for exact redacted
context rather than an unbounded content dump.

## Committed reproducible evidence

**Handler contract, redaction, and offset bounds** —
`go/internal/query/code_security_secrets_test.go`:
`TestHandleHardcodedSecretInvestigationReturnsRedactedPromptPacket` and
`TestHandleHardcodedSecretInvestigationRejectsHugeOffset`. Reproduce:

```bash
cd go && go test ./internal/query -run TestHandleHardcodedSecretInvestigation -count=1
```

## Notes

No private data: the cited tests use synthetic secret-pattern fixtures; the
capability itself redacts findings by contract, so this artifact does not
reproduce any real secret value.

Related: #5407 (artifact-existence gate), #5552 (burn-down).
