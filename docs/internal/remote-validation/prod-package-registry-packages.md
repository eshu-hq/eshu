# prod-package-registry-packages — production validation

Validation-Slug: prod-package-registry-packages
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: package_registry.packages.list passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


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
