# prod-answer-narration-status — production validation

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
