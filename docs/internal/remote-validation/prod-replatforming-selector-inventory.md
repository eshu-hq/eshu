# prod-replatforming-selector-inventory — production validation

Validation-Slug: prod-replatforming-selector-inventory
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: replatforming.selector_inventory passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


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
