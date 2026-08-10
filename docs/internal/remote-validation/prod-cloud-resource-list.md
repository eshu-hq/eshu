# prod-cloud-resource-list — production validation

Validation-Slug: prod-cloud-resource-list
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: platform_impact.cloud_resource_list passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.
B12-Assertion: platform_impact.cloud_resource_list -> http:GET /api/v0/cloud/resources

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


Capability: `platform_impact.cloud_resource_list` (tool `list_cloud_resources`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: optional_provider_resource_type_region_or_account_scope`,
`p95_latency_ms: 2500`, `max_truth_level: exact`.

## Claim validated

Bounded, filterable, keyset-paged list of `CloudResource` nodes ordered by `resource_type` then
`id`; `resource_type` and `arn` are indexed and `id` is unique so cursor resume stays selective.

## Committed reproducible evidence

**Handler happy path, limits, and provider filters** — `go/internal/query/cloud_resources_test.go`:
`TestListCloudResourcesHappyPath`, `TestListCloudResourcesEmpty`,
`TestListCloudResourcesLimitValidation`, `TestListCloudResourcesTruncationAndCursor`,
`TestListCloudResourcesCursorAppliesKeysetPredicate`,
`TestListCloudResourcesRejectsIncompleteCursor`, `TestListCloudResourcesProviderFilter`,
`TestListCloudResourcesBackendUnavailable`, and `TestListCloudResourcesQueryError`. Reproduce:

```bash
cd go && go test ./internal/query -run TestListCloudResources -count=1
```

**Keyset-page authorization ordering and hydration safety** —
`go/internal/query/cloud_resources_paging_test.go`:
`TestListCloudResourcesSelectsAuthorizedPageBeforeGraphHydration`,
`TestListCloudResourcesRejectsMalformedCursorBeforeReads`,
`TestListCloudResourcesEmptyScopedGrantShortCircuits`,
`TestListCloudResourcesFailsClosedOnHydrationDrift`, and
`TestListCloudResourcesPageCardinalities`. Reproduce:

```bash
cd go && go test ./internal/query -run TestListCloudResourcesSelectsAuthorizedPageBeforeGraphHydration -count=1
cd go && go test ./internal/query -run "TestListCloudResourcesRejectsMalformedCursorBeforeReads|TestListCloudResourcesEmptyScopedGrantShortCircuits|TestListCloudResourcesFailsClosedOnHydrationDrift|TestListCloudResourcesPageCardinalities" -count=1
```

**Store query construction (authorization before limit, indexed ordering)** —
`go/internal/query/cloud_resource_list_store_test.go`:
`TestBuildCloudResourceIdentityListQueryAppliesAuthorizationBeforeLimit`,
`TestBuildCloudResourceIdentityListQueryCoversEveryProductionVariant`, and
`TestBuildCloudResourceIdentityListQueryBindsValues`. Reproduce:

```bash
cd go && go test ./internal/query -run TestBuildCloudResourceIdentityListQuery -count=1
```

**Query-plan variant coverage** — `go/internal/query/queryplan_production_variants_test.go`:
`TestHandlerQueryplanCloudResourceListVariantsStayCovered`. Reproduce:

```bash
cd go && go test ./internal/query -run TestHandlerQueryplanCloudResourceListVariantsStayCovered -count=1
```

## Notes

No private data: fixtures use synthetic resource ARNs and provider/region values.

Related: #5552 (burn-down).
