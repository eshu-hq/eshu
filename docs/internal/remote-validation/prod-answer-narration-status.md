# prod-answer-narration-status — production validation

Validation-Slug: prod-answer-narration-status
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: answer_narration.status passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


Capability: `answer_narration.status` (tool `get_answer_narration_status`).
Production profile: `required_runtime: deployed_services`, `max_scope_size: runtime_status`,
`p95_latency_ms: 100`, `max_truth_level: derived`.

## Claim validated

Deployed answer narration status exposes only safe state, reason codes, fallback posture,
retention posture, and policy hash metadata — never a prompt, provider response, credential,
source identifier, graph read, content read, or provider call.

## Committed reproducible evidence

**Redacted status shape and default posture** — `go/internal/query/status_answer_narration_test.go`:
`TestStatusHandlerAnswerNarrationDefaultStatus` (redacted default posture with no prompt,
provider response, credential, or source identifier),
`TestStatusHandlerAnswerNarrationUsesInjectedPostureWhenSet`, and
`TestStatusHandlerAnswerNarrationDefaultClosedWhenNilPosture`. Reproduce:

```bash
cd go && go test ./internal/query -run TestStatusHandlerAnswerNarration -count=1
```

**Scoped-token route authorization** — same file:
`TestAuthMiddlewareWithScopedTokensAllowsAnswerNarrationStatusRoute`. Reproduce:

```bash
cd go && go test ./internal/query -run TestAuthMiddlewareWithScopedTokensAllowsAnswerNarrationStatusRoute -count=1
```

## Notes

No private data: tests assert absence of prompt/response/credential fields from the response
shape; no real provider or deployment values are used.

Related: #5552 (burn-down).
