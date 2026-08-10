# prod-entity-map — production validation

Validation-Slug: prod-entity-map
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: platform_impact.entity_map passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.
B12-Assertion: platform_impact.entity_map -> http:POST /api/v0/impact/entity-map

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


Capability: `platform_impact.entity_map` (tool `entity_map`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: multi_repo_platform`, `p95_latency_ms: 7000`,
`max_truth_level: exact`.

## Claim validated

Typed entity resolution plus bounded relationship-family graph traversal
(depth-bounded, single connected match, typed verb/entity-ID population on
variable-length edges), including Terraform-address resolution without a
whole-graph scan.

## Committed reproducible evidence

**Handler contract, bounded traversal, and depth control** —
`go/internal/query/entity_map_test.go`:
`TestEntityMapReturnsAmbiguityWithoutTraversal`,
`TestEntityMapUsesTypedAnchorAndGroupsBoundedNeighborhood`,
`TestEntityMapDepthTwoUsesBoundedTraversalSpecs`,
`TestEntityMapTraversalAnchorsExpansionInSingleConnectedMatch`,
`TestEntityMapResolvesTerraformAddressWithoutWholeGraphScan`, and
`TestEntityMapPopulatesTypedVerbAndEntityIDForVarLengthEdge`. Reproduce:

```bash
cd go && go test ./internal/query -run TestEntityMap -count=1
```

**Query-plan safety proof (entity-map traversal is one of the 16 registered
handler plan shapes)** — `docs/internal/evidence/5270-queryplan-handler-coverage.md`
documents an isolated 362/362-shape live `PROFILE` pass on Neo4j confirming
no `AllNodesScan`/`CartesianProduct`/unbounded expansion in the registered
entity-map traversal shape. Reproduce (requires Docker):

```bash
scripts/verify-query-plan-profile.sh
```

## Notes

No private data: cited tests use synthetic graph fixtures; the query-plan
proof runs against an empty schema-only proof graph with zero terminal
result rows.

Related: #5407 (artifact-existence gate), #5552 (burn-down).
