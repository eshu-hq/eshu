# prod-package-registry-aggregate — production validation

Capability: `package_registry.packages.aggregate` (tools
`count_package_registry_packages`, `get_package_registry_package_inventory`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: optional_ecosystem_registry_namespace_package_manager_or_visibility_scope`,
`p95_latency_ms: 1500`, `max_truth_level: exact`.

## Claim validated

Bounded `(:Package)` aggregate (count and grouped inventory by ecosystem,
registry, namespace, package_manager, or visibility) anchored on indexed
properties, including scoped-token visibility forcing to public-only.

## Committed reproducible evidence

**Rollup, bucketing, truncation, and contract validation** —
`go/internal/query/package_registry_aggregates_test.go`:
`TestCountPackageRegistryPackagesReturnsRollup`,
`TestPackageRegistryPackageInventoryReturnsBuckets`,
`TestPackageRegistryAggregateInventoryReportsTruncated`,
`TestPackageRegistryAggregateRejectsOutOfContractVisibility`,
`TestPackageRegistryAggregateAcceptsContractVisibilityValues`,
`TestPackageRegistryAggregateInventoryRejectsUnknownDimension`,
`TestPackageRegistryAggregateRoutesReturn503WhenStoreMissing`. Reproduce:

```bash
cd go && go test ./internal/query -run TestPackageRegistryAggregate -count=1
cd go && go test ./internal/query -run TestCountPackageRegistryPackages -count=1
```

**Scoped-token visibility forcing** —
`go/internal/query/package_registry_aggregates_scoped_test.go`:
`TestPackageRegistryAggregateCountScopedForcesPublicVisibility`,
`TestPackageRegistryAggregateCountScopedPrivateFilterReturnsEmptyWithoutStoreRead`,
`TestPackageRegistryAggregateInventoryScopedForcesPublicVisibilityAndDegeneratesGroupByVisibility`.

## Notes

No private data: this artifact cites only committed tests, no
deployment-specific values.

Related: #5552 (burn-down), #5407 (artifact-existence gate).
