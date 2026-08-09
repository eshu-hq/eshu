# prod-content-search — production validation

Validation-Slug: prod-content-search
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: code_search.content_search passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


Capability: `code_search.content_search` (tool `search_file_content`).
Production profile: `required_runtime: deployed_services`, `max_scope_size: multi_repo_platform`,
`p95_latency_ms: 1200`, `max_truth_level: derived`.

## Claim validated

Relational content search served from the Postgres content store, with cross-repository search,
case-sensitive exact matching, and hybrid reranking of results.

## Committed reproducible evidence

**Cross-repository content search and readiness classification** —
`go/internal/query/content_reader_cross_repo_test.go`:
`TestContentReaderSearchFileContentAnyRepo`,
`TestContentReaderSearchFileContentAnyRepoExactCaseUsesCaseSensitiveLike`,
`TestContentReaderSearchFileContentAnyRepoPageRequiresSubstringIndexesReady`,
`TestContentReaderSearchFileContentAnyRepoPageClassifiesReadinessFailure`, and
`TestContentReaderSearchFileContentAnyRepoDefaultsLimit`. Reproduce:

```bash
cd go && go test ./internal/query -run TestContentReaderSearchFileContent -count=1
```

**Hybrid rerank of content search results** — `go/internal/query/content_hybrid_rerank_test.go`:
`TestSearchFileContentResultsAreHybridReranked` and
`TestSearchFileContentWithEmptyBodiesKeepsLexicalOrder`. Reproduce:

```bash
cd go && go test ./internal/query -run TestSearchFileContent -count=1
```

**Scoped-token authorization** — `go/internal/query/content_handler_authz_test.go`:
`TestContentHandlerAllScopeContentSearchKeepsAnyRepoFallback`. Reproduce:

```bash
cd go && go test ./internal/query -run TestContentHandlerAllScopeContentSearchKeepsAnyRepoFallback -count=1
```

**Deferred-index readiness contract** — `go/internal/query/openapi_content_index_readiness_test.go`:
`TestOpenAPISpecContentSearchDocumentsDeferredIndexUnavailable`. Reproduce:

```bash
cd go && go test ./internal/query -run TestOpenAPISpecContentSearchDocumentsDeferredIndexUnavailable -count=1
```

## Notes

No private data: fixtures use synthetic repository/file content only.

Related: #5552 (burn-down).
