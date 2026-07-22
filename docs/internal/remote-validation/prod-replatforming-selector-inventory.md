# prod-replatforming-selector-inventory — production validation

Capability: `replatforming.selector_inventory` (API-only; no MCP tool
mounted — the matrix row's `tools` field is empty).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: bounded_active_aws_collector_scope_page`,
`p95_latency_ms: 2000`, `max_truth_level: derived`.

## Claim validated

Bounded active-AWS-scope inventory with indexed active-generation finding
counts; exact scope-grant filtering includes authoritative zero-finding
scopes and never scans superseded collector generations.

## Committed reproducible evidence

**Handler bounds, missing-collector-evidence distinction, scoped AWS
grants** — `go/internal/query/replatforming_selectors_handler_test.go`:
`TestReplatformingSelectorsHandlerListsBoundedAuthorizedChoices`,
`TestReplatformingSelectorsHandlerDistinguishesMissingCollectorEvidence`,
`TestReplatformingSelectorsHandlerPassesScopedAWSGrantsToStore`,
`TestReplatformingSelectorsHandlerRejectsScopedNonAWSGrantsWithoutStoreRead`,
`TestReplatformingSelectorsHandlerReturnsEmptyWithoutStoreForScopedRepositoryOnlyGrant`.
Reproduce:

```bash
cd go && go test ./internal/query -run TestReplatformingSelectorsHandler -count=1
```

**Postgres store: active-generation scoping and exact-grant filtering** —
`go/internal/storage/postgres/replatforming_selectors_test.go`:
`TestAWSCloudRuntimeDriftFindingStoreListsActiveReplatformingScopes`,
`TestAWSCloudRuntimeDriftFindingStoreScopesReplatformingSelectorsToExactGrants`.
Reproduce:

```bash
cd go && go test ./internal/storage/postgres -run TestAWSCloudRuntimeDriftFindingStore -count=1
```

**OpenAPI contract declaration** —
`go/internal/query/openapi_replatforming_selectors_test.go`.

## Notes

No private data: this artifact cites only committed tests, no
deployment-specific values.

Related: #5552 (burn-down), #5407 (artifact-existence gate).
