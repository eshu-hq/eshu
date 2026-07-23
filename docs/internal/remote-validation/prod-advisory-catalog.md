# prod-advisory-catalog — production validation

Capability: `supply_chain.advisory_catalog.list` (HTTP route only; no MCP tool is registered
for this capability).
Production profile: `required_runtime: deployed_services`, `max_scope_size: bounded_catalog_page`,
`p95_latency_ms: 1500`, `max_truth_level: exact`.

## Claim validated

Browsable, summary-only CVE-intelligence catalog ordered by CVSS desc then advisory key with
keyset pagination; rows are known intelligence and do not imply repository, image, workload, or
deployment impact.

## Committed reproducible evidence

**Request validation, pagination, and filters** — `go/internal/query/supply_chain_advisory_catalog_test.go`:
`TestSupplyChainListAdvisoryCatalogRequiresLimit`, `TestSupplyChainListAdvisoryCatalogRejectsLimitOutOfRange`,
`TestSupplyChainListAdvisoryCatalogRejectsBadKEVAndCursor`,
`TestSupplyChainListAdvisoryCatalogPassesFiltersAndPaginates`, and
`TestSupplyChainListAdvisoryCatalogAcceptsCursor`. Reproduce:

```bash
cd go && go test ./internal/query -run TestSupplyChainListAdvisoryCatalog -count=1
```

**Active source fact read model and single-pass bounded shape** — same file:
`TestAdvisoryCatalogQueryUsesActiveSourceFactReadModel`,
`TestAdvisoryCatalogQueryUsesBoundedSinglePassShape`, and
`TestAdvisoryCatalogQueryKeepsPerFactKindActiveScanAnchor`. Reproduce:

```bash
cd go && go test ./internal/query -run TestAdvisoryCatalogQuery -count=1
```

**Backend readiness and contract declaration** — `TestSupplyChainListAdvisoryCatalogReturnsBackendUnavailable`,
`TestPostgresAdvisoryCatalogStoreRejectsPaginationLimit`, `TestPostgresAdvisoryCatalogStoreRequiresDB`
(same file), and `TestOpenAPISpecIncludesAdvisoryCatalog`. Reproduce:

```bash
cd go && go test ./internal/query -run "TestPostgresAdvisoryCatalogStore|TestOpenAPISpecIncludesAdvisoryCatalog" -count=1
```

## Notes

No private data: rows exercised in tests are CVE/advisory intelligence fixtures, never
repository, image, or deployment-specific identifiers.

Related: #5552 (burn-down).
