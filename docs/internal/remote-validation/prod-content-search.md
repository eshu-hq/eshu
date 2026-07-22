# prod-content-search — production validation

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
