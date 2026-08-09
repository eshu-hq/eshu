# prod-code-search-exact — production validation

Validation-Slug: prod-code-search-exact
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: code_search.exact_symbol passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


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
