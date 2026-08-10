# prod-read-only-cypher — production validation

Validation-Slug: prod-read-only-cypher
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: graph_query.read_only_cypher passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.
B12-Assertion: graph_query.read_only_cypher -> mcp:execute_cypher_query

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


Capability: `graph_query.read_only_cypher` (tool `execute_cypher_query`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: bounded_query_window`, `p95_latency_ms: 2000`,
`max_truth_level: exact`. Diagnostics-only read path; purpose-built tools
are preferred for prompt contracts.

## Claim validated

Read-only Cypher capped by server-side `LIMIT` and deadline/timeout; mutation
keywords are rejected before any graph read, including limits smuggled
inside string literals.

## Committed reproducible evidence

**Mutation rejection, bounded limit/deadline enforcement** —
`go/internal/query/code_cypher_handler_test.go`:
`TestValidateReadOnlyCypher_RejectsMutationKeywords`,
`TestValidateReadOnlyCypher_AllowsReadOnlyQueries`,
`TestValidateReadOnlyCypher_RejectsLongQueries`,
`TestHandleCypherQuery_RejectsMutations`,
`TestHandleCypherQueryRejectsMutationWithEnvelopeError`,
`TestHandleCypherQueryRejectsUnsupportedProfileBeforeGraph`,
`TestHandleCypherQuery_ExecutesReadOnlyQuery`,
`TestHandleCypherQueryPassesDeadlineToGraph`,
`TestHandleCypherQueryAddsBoundedLimitAndEnvelope`,
`TestHandleCypherQueryRejectsExplicitLimitAboveRequestedLimit`,
`TestHandleCypherQueryIgnoresLimitInsideStringLiteral`. Reproduce:

```bash
cd go && go test ./internal/query -run TestHandleCypherQuery -count=1
cd go && go test ./internal/query -run TestValidateReadOnlyCypher -count=1
```

**Additional validation coverage** — `go/internal/query/code_cypher_test.go`.

## Notes

No private data: this artifact cites only committed tests, no
deployment-specific values.

Related: #5552 (burn-down), #5407 (artifact-existence gate).
