# prod-package-registry-correlations — production validation

Validation-Slug: prod-package-registry-correlations
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: package_registry.correlations.list passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


Capability: `package_registry.correlations.list` (tool
`list_package_registry_correlations`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: package_or_repository_id`, `p95_latency_ms: 1500`,
`max_truth_level: exact`.

## Claim validated

Bounded reducer package ownership, publication, and consumption correlation
lookup anchored by `Package.uid` or `Repository.id`, excluding tombstoned
rows and supporting batched package IDs and relationship-kind filters.

## Committed reproducible evidence

**Handler bounds, store, and query filters** —
`go/internal/query/package_registry_correlations_test.go`:
`TestPackageRegistryListCorrelationsRequiresScopeAndLimit`,
`TestPackageRegistryListCorrelationsUsesBoundedPostgresStore`,
`TestPackageRegistryCorrelationQueryExcludesTombstones`,
`TestPackageRegistryCorrelationQuerySupportsBatchedPackageIDs`,
`TestPackageRegistryCorrelationQuerySupportsRelationshipKindsFilter`,
`TestPackageRegistryCorrelationQueryIncludesPublicationFacts`,
`TestPackageRegistryCorrelationsResolveRepositorySelectors`,
`TestPackageRegistryCorrelationsRejectUnknownRepositorySelector`,
`TestPackageRegistryCorrelationSQLAppliesScopedAuthorizationBeforeOrder`.
Reproduce:

```bash
cd go && go test ./internal/query -run TestPackageRegistryListCorrelations -count=1
cd go && go test ./internal/query -run TestPackageRegistryCorrelation -count=1
```

## Notes

No private data: this artifact cites only committed tests, no
deployment-specific values.

Related: #5552 (burn-down), #5407 (artifact-existence gate).
