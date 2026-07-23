# prod-package-registry-versions — production validation

Capability: `package_registry.versions.list` (tool
`list_package_registry_versions`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: package_id`, `p95_latency_ms: 1500`,
`max_truth_level: exact`.

## Claim validated

Package-version identity lookup anchored by `Package.uid`, bounded by scope
and limit.

## Committed reproducible evidence

**Handler bounds and anchoring** —
`go/internal/query/package_registry_test.go`:
`TestPackageRegistryListVersionsRequiresPackageScopeAndLimit`,
`TestPackageRegistryListVersionsUsesPackageUIDAnchor`. Reproduce:

```bash
cd go && go test ./internal/query -run TestPackageRegistryListVersions -count=1
```

**Shared live version-count correctness fix** —
`docs/internal/evidence/5167-package-registry-version-count-nornicdb.md`
(fixes the shared `OPTIONAL MATCH ... count(v)` version-aggregation defect on
the pinned NornicDB build that both the packages and versions surfaces
depend on).

## Notes

No private data: this artifact cites only committed tests and a committed
evidence note, no deployment-specific values.

Related: #5552 (burn-down), #5407 (artifact-existence gate).
