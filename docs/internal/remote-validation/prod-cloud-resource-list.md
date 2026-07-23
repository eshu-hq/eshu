# prod-cloud-resource-list — production validation

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
