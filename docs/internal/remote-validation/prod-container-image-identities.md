# prod-container-image-identities — production validation

Capability: `supply_chain.container_image_identities.list` (tool
`list_container_image_identities`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: digest_ref_repository_or_outcome_scope`, `p95_latency_ms: 1500`,
`max_truth_level: exact`.

## Claim validated

Bounded reducer container image identity lookup anchored by digest, image ref, repository id, or
outcome.

## Committed reproducible evidence

**Scope/limit validation and bounded store lookup** — `go/internal/query/container_image_identities_test.go`:
`TestSupplyChainListContainerImageIdentitiesRequiresScopeAndLimit`,
`TestSupplyChainListContainerImageIdentitiesRejectsUnsupportedOutcome`,
`TestSupplyChainListContainerImageIdentitiesUsesBoundedStore`,
`TestPostgresContainerImageIdentityStoreReportsPaginationLimit`, and
`TestContainerImageIdentityQueryUsesActiveFactReadModel`. Reproduce:

```bash
cd go && go test ./internal/query -run TestSupplyChainListContainerImageIdentities -count=1
cd go && go test ./internal/query -run TestContainerImageIdentityQueryUsesActiveFactReadModel -count=1
```

**Source-repository bridge anchoring** — `go/internal/query/container_image_identities_source_bridge_test.go`:
`TestSupplyChainListContainerImageIdentitiesUsesSourceRepositoryBridge` and
`TestContainerImageIdentityQueryUsesSourceRepositoryAnchor`. Reproduce:

```bash
cd go && go test ./internal/query -run TestContainerImageIdentity.*SourceRepository -count=1
cd go && go test ./internal/query -run TestSupplyChainListContainerImageIdentitiesUsesSourceRepositoryBridge -count=1
```

**Scoped-token authorization** — `go/internal/query/container_image_identity_scope_test.go`:
`TestAuthMiddlewareWithScopedTokensAllowsContainerImageIdentityRoutes`,
`TestContainerImageIdentityScopedEmptyGrantReturnsEmptyWithoutStoreRead`,
`TestContainerImageIdentityScopedSourceSelectorDeniesOutOfGrantWithoutStoreRead`, and
`TestContainerImageIdentitySQLAppliesSourceRepositoryGrantOverlap`. Reproduce:

```bash
cd go && go test ./internal/query -run TestContainerImageIdentity -count=1
```

**Contract declaration** — `go/internal/query/openapi_supply_chain_test.go`:
`TestOpenAPISpecIncludesContainerImageIdentities` and
`TestOpenAPISpecIncludesContainerImageSourceRepositoryBridge`. Reproduce:

```bash
cd go && go test ./internal/query -run TestOpenAPISpecIncludesContainerImage -count=1
```

## Notes

No private data: fixtures use synthetic image digests, refs, and repository IDs only.

Related: #5552 (burn-down).
