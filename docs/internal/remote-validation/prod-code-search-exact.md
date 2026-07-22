# prod-code-search-exact — production validation

Capability: `code_search.exact_symbol` (tool `find_code`).
Production profile: `required_runtime: deployed_services`, `max_scope_size: multi_repo_platform`,
`p95_latency_ms: 800`, `max_truth_level: exact`.

## Claim validated

Repository-selected indexed graph lookup or global content entity-name lookup, using the
`exact` request flag on the `find_code` (`/api/v0/code/search`) handler.

## Committed reproducible evidence

**Exact-match pagination and overflow reporting** — `go/internal/query/code_search_pagination_test.go`:
`TestGlobalCodeSearchReportsExactAndOverflowPages` and
`TestGlobalCodeSearchMaximumPublicLimitUsesOneRowProbe` (both issue `{"exact":true}` requests).
Reproduce:

```bash
cd go && go test ./internal/query -run TestGlobalCodeSearchReportsExactAndOverflowPages -count=1
cd go && go test ./internal/query -run TestGlobalCodeSearchMaximumPublicLimitUsesOneRowProbe -count=1
```

**Repository-selected exact lookup with scoped authorization** — `go/internal/query/code_search_authz_test.go`:
`TestCodeSearchCanonicalRepositoryStartsFromIndexedRepository` (issues an `{"exact":true}`
request against a repo-scoped selector). Reproduce:

```bash
cd go && go test ./internal/query -run TestCodeSearchCanonicalRepositoryStartsFromIndexedRepository -count=1
```

**Scoped-token route allowlisting** — `go/internal/query/auth_scoped_route_gate_test.go`:
`TestAuthMiddlewareWithScopedTokensAllowsCodeSearchWithEmptyGrant`. Reproduce:

```bash
cd go && go test ./internal/query -run TestAuthMiddlewareWithScopedTokensAllowsCodeSearchWithEmptyGrant -count=1
```

## Notes

No private data: fixtures use synthetic entity names and repository IDs only.

Related: #5552 (burn-down).
