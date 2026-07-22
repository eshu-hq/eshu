# prod-infra-resource-aggregate — production validation

Capability: `platform_impact.infra_resource_aggregate` (tools
`count_infra_resources`, `get_infra_resource_inventory`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: optional_category_provider_environment_or_resource_service_scope`,
`p95_latency_ms: 2500`, `max_truth_level: exact`.

## Claim validated

Bounded infrastructure resource aggregate (count and grouped inventory by
provider, environment, resource_category, resource_service, or label) over
the documented infrastructure labels, anchored on indexed properties.

## Committed reproducible evidence

**Handler bounds, rollups, and scoped-grant filtering** —
`go/internal/query/infra_resource_aggregates_test.go`:
`TestInfraResourceAggregateCountReturnsRollups`,
`TestInfraResourceAggregateInventoryReturnsBuckets`,
`TestInfraResourceAggregateInventoryReportsTruncated`,
`TestInfraResourceAggregateInventoryRejectsUnknownDimension`,
`TestInfraResourceAggregateRoutesReturn503WhenStoreMissing`. Reproduce:

```bash
cd go && go test ./internal/query -run TestInfraResourceAggregate -count=1
```

**Category acceptance and indexed-property WHERE-clause shape** —
`go/internal/query/infra_resource_aggregates_category_test.go`:
`TestInfraResourceAggregateAcceptsCloudCategory`,
`TestInfraResourceAggregateRejectsUnknownCategory`;
`go/internal/query/infra_resource_aggregates_where_test.go`:
`TestInfraResourceAggregateWhereClauseUsesDirectEqualityForIndexedProps`.
Reproduce:

```bash
cd go && go test ./internal/query -run TestInfraResourceAggregate -count=1
```

**Scoped-grant array binding** —
`go/internal/query/infra_resource_aggregates_scope_test.go`:
`TestInfraResourceAggregateScopedEmptyGrantReturnsEmptyWithoutStoreRead`,
`TestInfraResourceAggregateScopedGrantPropagatesToFilter`,
`TestInfraResourceAggregateParamsBindGrantArraysWhenScoped`.

**Live per-label-anchoring correctness fix** —
`docs/internal/evidence/5280-5281-infra-aggregate-and-code-flow-index.md`
(graph infra aggregates anchoring, part of the #5267 console-recovery epic)
and `docs/internal/evidence/5384-infra-scope-shape-a.md` (scoped-token
authorization predicate fix for `infra/resources/count` and `/inventory`).

## Notes

No private data: this artifact cites only committed tests and committed
evidence notes, no deployment-specific values.

Related: #5552 (burn-down), #5407 (artifact-existence gate).
