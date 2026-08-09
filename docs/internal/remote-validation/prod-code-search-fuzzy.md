# prod-code-search-fuzzy — production validation

Validation-Slug: prod-code-search-fuzzy
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: code_search.fuzzy_symbol passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


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
