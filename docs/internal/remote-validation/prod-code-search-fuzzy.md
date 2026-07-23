# prod-code-search-fuzzy — production validation

Capability: `code_search.fuzzy_symbol` (tool `find_code`).
Production profile: `required_runtime: deployed_services`, `max_scope_size: multi_repo_platform`,
`p95_latency_ms: 1000`, `max_truth_level: derived`.

## Claim validated

Bounded case-sensitive literal substring lookup on the `find_code` (`/api/v0/code/search`)
handler when the `exact` request flag is unset; this capability does not claim fuzzy scoring.

## Committed reproducible evidence

**Substring (non-exact) search with scoped authorization** — `go/internal/query/code_search_authz_test.go`:
`TestCodeSearchGraphAppliesScopedAuthBeforeLimit`,
`TestCodeSearchContentAppliesScopedAuthWithoutAnyRepoFallback`,
`TestCodeSearchContentEmptyGrantReturnsEmptyWithoutBroadScan`,
`TestCodeSearchAllScopeAdminKeepsAnyRepoFallback`,
`TestCodeSearchScopedSelectorFiltersDuplicateRepositoryNames`, and
`TestCodeSearchScopedSelectorDeniesOutOfScopeCanonicalID` (all issue non-`exact` requests).
Reproduce:

```bash
cd go && go test ./internal/query -run TestCodeSearch -count=1
```

**Global substring bound and content-name-index authorization** — `go/internal/query/entity_name_search_test.go`:
`TestGlobalCodeSearchUsesOneAuthorizedContentNameQuery` and
`TestGlobalCodeSearchRequiresBoundedSubstringAndNameStore` (asserts the "at least three Unicode
characters" global substring bound). Reproduce:

```bash
cd go && go test ./internal/query -run "TestGlobalCodeSearchUsesOneAuthorizedContentNameQuery|TestGlobalCodeSearchRequiresBoundedSubstringAndNameStore" -count=1
```

**Hybrid rerank of substring content results** — `go/internal/query/code_hybrid_rerank_test.go`:
`TestCodeSearchContentResultsAreHybridReranked` and
`TestCodeSearchHybridRerankFallsBackToLexicalOrder`. Reproduce:

```bash
cd go && go test ./internal/query -run TestCodeSearch.*HybridRerank -count=1
```

## Notes

No private data: fixtures use synthetic entity names and repository IDs only.

Related: #5552 (burn-down).
