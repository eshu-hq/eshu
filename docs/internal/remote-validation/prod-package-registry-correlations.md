# prod-package-registry-correlations — production validation

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
