# prod-infra-resource-aggregate — production validation

Validation-Slug: prod-infra-resource-aggregate
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu5552-ask-aggregate-20260809c ESHU_POSTGRES_PORT=25432 NORNICDB_BOLT_PORT=27687 NORNICDB_HTTP_PORT=27474 GATE_API_PORT=28080 GATE_MCP_PORT=28091 GATE_PROMETHEUS_SOURCE_PORT=29090 GATE_ASK_PROVIDER_PORT=29191 bash scripts/verify-golden-corpus-gate.sh > /tmp/eshu-5552-b7-ask-aggregate-20260809c.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: platform_impact.infra_resource_aggregate passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.
B12-Assertion: platform_impact.infra_resource_aggregate -> mcp:count_infra_resources

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. Before API and MCP validation, the gate counted the explicit infrastructure taxonomy and provider properties directly from the persisted graph through its independent Bolt oracle and substituted those measured values into the runtime snapshot. The gate completed with 550 passes, zero required failures, and zero advisory warnings in 121 seconds.


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
