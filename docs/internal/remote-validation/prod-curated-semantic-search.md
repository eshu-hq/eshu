# prod-curated-semantic-search — production validation

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
