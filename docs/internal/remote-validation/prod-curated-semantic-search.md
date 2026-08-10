# prod-curated-semantic-search — production validation

Validation-Slug: prod-curated-semantic-search
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: semantic_search.curated_retrieval passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.
B12-Assertion: semantic_search.curated_retrieval -> mcp:search_semantic_context

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


Capability: `semantic_search.curated_retrieval` (tool `search_semantic_context`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: active_repo`, `p95_latency_ms: 1500`,
`max_truth_level: derived`.

## Claim validated

Bounded, repository-scoped curated search-document retrieval over active
generations, with explicit `limit`, `timeout`, truncation, `search_method`,
and `derived` truth labeling; scoped-token filtering is applied before
read-model access, and results are bounded retrieval evidence only (never
promoted to canonical graph truth).

## Committed reproducible evidence

**Handler contract, bounds, and scoped-token gating** —
`go/internal/query/semantic_search_test.go`:
`TestSemanticSearchHandlerReturnsBoundedTruthLabeledResults`,
`TestSemanticSearchHandlerRejectsUnboundedRequestsBeforeRead`,
`TestSemanticSearchHandlerScopedEmptyGrantReturnsEmptyWithoutRead`, and
`TestSemanticSearchHandlerScopedGrantRejectsOutOfGrantRepositoryBeforeRead`.
Reproduce:

```bash
cd go && go test ./internal/query -run TestSemanticSearchHandler -count=1
```

**Hybrid/local-embedder degradation honesty** —
`go/internal/query/semantic_search_hybrid_test.go`:
`TestSemanticSearchHandlerConfiguredHybridReportsHybridParticipation` and
`TestSemanticSearchHandlerHybridWithoutLocalEmbedderReportsDegradedKeywordState`
(reports `bm25` rather than silently claiming vector search when no local
embedder is configured, matching the production-profile note).

**Route authorization** —
`go/internal/query/semantic_search_test.go`:
`TestAuthMiddlewareWithScopedTokensAllowsSemanticSearchRoute`.

## Notes

No private data: cited tests use synthetic search-document fixtures; no raw
prompt payloads or provider responses appear in this artifact, consistent
with the capability's own redaction contract.

Related: #5407 (artifact-existence gate), #5552 (burn-down).
