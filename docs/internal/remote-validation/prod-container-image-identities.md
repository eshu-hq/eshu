# prod-container-image-identities — production validation

Validation-Slug: prod-container-image-identities
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: supply_chain.container_image_identities.list passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.
B12-Assertion: supply_chain.container_image_identities.list -> mcp:list_container_image_identities

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


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
