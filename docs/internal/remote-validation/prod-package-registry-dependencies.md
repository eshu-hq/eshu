# prod-package-registry-dependencies — production validation

Capability: `package_registry.dependencies.list` (tool
`list_package_registry_dependencies`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: package_or_version_id`, `p95_latency_ms: 1500`,
`max_truth_level: exact`.

## Claim validated

Bounded package-native dependency lookup anchored by `Package.uid` or
`PackageVersion.uid`, with cursor-paginated truncation.

## Committed reproducible evidence

**Handler bounds, anchor combinations, and pagination** —
`go/internal/query/package_registry_dependencies_test.go`:
`TestPackageRegistryListDependenciesRequiresScopeAndLimit`,
`TestPackageRegistryListDependenciesUsesPackageOrVersionAnchor`,
`TestPackageRegistryListDependenciesReturnsEmptySparsePackageQuickly`,
`TestPackageRegistryListDependenciesUsesBothAnchorsWhenProvided`,
`TestPackageRegistryListDependenciesReturnsCursorForTruncatedPage`.
Reproduce:

```bash
cd go && go test ./internal/query -run TestPackageRegistryListDependencies -count=1
```

## Notes

No private data: this artifact cites only committed tests, no
deployment-specific values.

Related: #5552 (burn-down), #5407 (artifact-existence gate).
