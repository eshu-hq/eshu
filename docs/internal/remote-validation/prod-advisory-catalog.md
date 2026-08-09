# prod-advisory-catalog — production validation

Validation-Slug: prod-advisory-catalog
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: supply_chain.advisory_catalog.list passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


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
