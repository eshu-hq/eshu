# prod-infra-resource-aggregate — production validation

Validation-Slug: prod-infra-resource-aggregate
Validation-Tier: deployed_services
Validation-Date: 2026-09-19
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: scripts/verify-golden-corpus-gate.sh; echo $?
Validation-Exit-Code: 0
Capability-Assertion: platform_impact.infra_resource_aggregate passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.
B12-Assertion: platform_impact.infra_resource_aggregate -> mcp:count_infra_resources

## Fresh deployed validation

A fresh credential-free Compose run at head `09986d86c` (branch of #6793,
PR #6817) rebuilt every binary, replayed all cassette scope generations,
drained the projector and reducer to terminal state, and exercised the
committed B-12 API/MCP assertion for this capability. The gate exited 0 with
560 passes, 0 required failures, and 2 advisory warnings. The pipeline took
228 s, and 326 s of wall time including the build, against its 1,800 s
budget ceiling. `[PASS] mcp:count_infra_resources` compared the
`by_provider.aws` and `by_provider.gcp` counts against the gate's Bolt oracle;
`total_resources` is asserted present, not compared.

The two advisory warnings are timing only: `phase_bootstrap` took 12 s against
a 10 s ceiling, and `phase_maintenance_drains` took 79 s against 30 s. main
at a37709893, run with the same gate on the same machine straight afterwards,
showed the same warnings (`phase_bootstrap` 12 s, `phase_maintenance_drains`
79 s, and also `phase_graph_query` 10 s), so they reflect machine load, not
this change.

This run covers the hybrid read path of the #6793 read model. A second run
at the same head with `--keep` (559 passes, 0 required failures, 3 advisory
warnings) left the stack up for inspection:

- The API log records `infra_inventory.backfill completed` at API startup,
  before the query phase: 14 repositories, 110 rows inserted, marker
  `infra_resource_entities_v2`.
- Postgres afterwards held 110 `infra_resource_entities` rows, 0
  `infra_resource_entity_dirty_repos` rows, and the backfill marker row. The
  read model was ready: marker present, no fence marks.
- An unscoped `GET /api/v0/infra/resources/count` against that stack returned
  200 with truth basis `hybrid` and level `derived`, with a reason naming the
  Postgres read model, and `total_resources` 239 across 20 labels.

The B-12 `count_infra_resources` assertion therefore ran on the hybrid path.
Content-derived labels came from the Postgres `infra_resource_entities`
table; `CloudResource`, `TerraformStateResource`, and the Terraform state
projector's `TerraformModule` and `TerraformOutput` nodes came from one graph
pass. The gate's independent Bolt oracle counts the infrastructure taxonomy
and provider properties directly from the persisted graph and substitutes
those counts into the runtime snapshot before the API and MCP assertions run.
The pass therefore proves `by_provider` aws and gcp count parity, summed over
every infra label and so drawn from both the table leg and the graph leg, on
this corpus. `total_resources` is asserted present, not compared, and
`by_label` and `by_environment` are not compared.

This run does NOT measure production p95 latency. The profile's
`p95_latency_ms: 2500` is a budget (see
`docs/public/reference/capability-conformance-spec.md`), and the first
production p95 for the hybrid path is recorded after deploy. At larger scale,
the cold first request still scans the graph-only labels whole, and one
latency-gate run returned HTTP 504 on that first request (#6843).

Capability: `platform_impact.infra_resource_aggregate` (tools
`count_infra_resources`, `get_infra_resource_inventory`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: optional_category_provider_environment_or_resource_service_scope`,
`p95_latency_ms: 2500` (budget), `max_truth_level: exact`.

## Claim validated

Bounded infrastructure resource aggregate (count and grouped inventory by
provider, environment, resource_category, resource_service, or label) over
the documented infrastructure labels. Unscoped reads, once the read model is
ready (backfill marker present and no rolling-upgrade fence mark), come from
the Postgres read model plus one indexed graph pass (truth basis `hybrid`, or
`content_index` for table-only categories). Scoped reads, reads before the
read model is ready, and `category=cloud` come from the per-label graph path
(`authoritative_graph`).

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

**Read model routing, parity, and fence** —
`go/internal/query/infra_resource_aggregates_read_model_test.go`,
`go/internal/query/infra_resource_aggregates_read_model_readiness_test.go`,
the live proofs in `go/internal/storage/postgres/infra/inventory/`, and the
evidence notes `docs/internal/evidence/6793-infra-read-model.md` and
`docs/internal/evidence/6793-infra-read-model-fence.md` (graph against table
differential, drift reconcile, and the rolling-upgrade fence).

```bash
cd go && go test ./internal/query -run 'TestInfraAggregate|TestInfraResourceAggregate' -count=1
```

**Live per-label-anchoring correctness fix** —
`docs/internal/evidence/5280-5281-infra-aggregate-and-code-flow-index.md`
(graph infra aggregates anchoring, part of the #5267 console-recovery epic)
and `docs/internal/evidence/5384-infra-scope-shape-a.md` (scoped-token
authorization predicate fix for `infra/resources/count` and `/inventory`).

## Notes

No private data: this artifact cites only committed tests, committed evidence
notes, and a credential-free Compose run, with no deployment-specific values.

Related: #6793 (hybrid read model), #6843 (cold graph-only first hit), #5552
(burn-down), #5407 (artifact-existence gate).
