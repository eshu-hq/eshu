# prod-catalog — production validation

Validation-Slug: prod-catalog
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: platform_impact.catalog passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.
B12-Assertion: platform_impact.catalog -> mcp:get_ecosystem_overview

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


Capability: `platform_impact.catalog` (tools `get_ecosystem_overview`,
`count_repositories_by_language`, `list_repositories_by_language`,
`get_repository_language_inventory`).
Production profile: `required_runtime: deployed_services`, `max_scope_size: multi_repo_platform`,
`p95_latency_ms: 3000`, `max_truth_level: exact`.

## Claim validated

Bounded repository, workload, and service handles from the authoritative graph, with
independent per-label counts and repository-language inventory aggregation.

## Committed reproducible evidence

**Ecosystem overview counting and scoped-token authorization** —
`go/internal/query/infra_ecosystem_overview_test.go`:
`TestGetEcosystemOverviewCountsEachLabelIndependently`,
`TestGetEcosystemOverviewScopedEmptyGrantReturnsAllZeroCountsWithoutGraphRead`,
`TestGetEcosystemOverviewScopedGrantBindsWhereClauseAndReturnsRealRowData`,
`TestGetEcosystemOverviewUnscopedQueryStaysUnfiltered`, and
`TestGetEcosystemOverviewScopedGrantExcludesCollidedWorkloadInstancesAndPlatforms`. Reproduce:

```bash
cd go && go test ./internal/query -run TestGetEcosystemOverview -count=1
```

**Repository-language inventory aggregation** — `go/internal/query/repository_language_inventory_test.go`:
`TestGetRepositoryLanguageInventoryScopedEmptyGrantReturnsEmptyWithoutQuery`,
`TestGetRepositoryLanguageInventoryScopedGrantHitsRealStoreAndReturnsRowData`,
`TestRepositoryLanguageInventoryReturnsAggregates`, and `TestRepositoryLanguageFamilyAliases`;
content-store aggregate parity in `go/internal/query/content_reader_test.go`:
`TestContentReaderRepositoryLanguageInventoryReturnsAggregateRows`. Reproduce:

```bash
cd go && go test ./internal/query -run "TestGetRepositoryLanguageInventory|TestRepositoryLanguageInventoryReturnsAggregates|TestRepositoryLanguageFamilyAliases|TestContentReaderRepositoryLanguageInventoryReturnsAggregateRows" -count=1
```

**Scoped-token route allowlisting** — `go/internal/query/auth_browser_session_all_scopes_test.go`:
`TestAuthMiddlewareRestrictedCredentialsReachEcosystemOverviewRoute`, and
`go/internal/query/auth_scoped_routes_ask_test.go`:
`TestScopedHTTPRouteSupportsTenantFilterAllowsEcosystemOverview`. Reproduce:

```bash
cd go && go test ./internal/query -run "TestAuthMiddlewareRestrictedCredentialsReachEcosystemOverviewRoute|TestScopedHTTPRouteSupportsTenantFilterAllowsEcosystemOverview" -count=1
```

**Local-lightweight fallback and contract declaration** — `go/internal/query/contract_endpoint_test.go`:
`TestGetEcosystemOverview_LocalLightweightReturnsStructuredUnsupportedCapability`, and
`go/internal/query/openapi_repository_language_test.go`:
`TestOpenAPIRepositoryLanguageDocumentsCoverageFields`. Reproduce:

```bash
cd go && go test ./internal/query -run "TestGetEcosystemOverview_LocalLightweight|TestOpenAPIRepositoryLanguageDocumentsCoverageFields" -count=1
```

## Notes

No private data: all fixtures use synthetic repository, workload, and platform node IDs.

Related: #5552 (burn-down).
