# prod-package-registry-packages — production validation

Capability: `package_registry.packages.list` (tool
`list_package_registry_packages`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: bounded_registry_scope`, `p95_latency_ms: 1500`,
`max_truth_level: exact`.

## Claim validated

Bounded package identity lookup by `package_id` or ecosystem/name; source
ownership stays provenance-only (not asserted as an owning edge).

## Committed reproducible evidence

**Handler bounds and identity classification** —
`go/internal/query/package_registry_test.go`:
`TestPackageRegistryListPackagesRequiresScopeAndLimit`,
`TestPackageRegistryListPackagesNamesMissingEcosystem`,
`TestPackageRegistryListPackagesUsesIndexedPackageScopeAndTruncates`,
`TestPackageRegistryListPackagesReturns500WhenVersionCountReadFails`; and
`go/internal/query/package_registry_identity_test.go`:
`TestPackageRegistryListPackagesClassifiesBlankPackageIdentityRows`,
`TestPackageRegistryListPackagesPreservesZeroVersionNPMIdentities`.
Reproduce:

```bash
cd go && go test ./internal/query -run TestPackageRegistryListPackages -count=1
```

**Live NornicDB version-count correctness fix** —
`docs/internal/evidence/5167-package-registry-version-count-nornicdb.md`
(fixes `GET /api/v0/package-registry/packages`'
`OPTIONAL MATCH ... count(v)` group-collapse defect on the pinned NornicDB
build).

## Notes

No private data: this artifact cites only committed tests and a committed
evidence note, no deployment-specific values.

Related: #5552 (burn-down), #5407 (artifact-existence gate).
