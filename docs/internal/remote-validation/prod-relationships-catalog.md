# prod-relationships-catalog — production validation

Validation-Slug: prod-relationships-catalog
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: platform_impact.relationships_catalog passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.
B12-Assertion: platform_impact.relationships_catalog -> mcp:list_relationship_edges

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


Capability: `platform_impact.relationships_catalog` (tools
`get_relationships_catalog`, `get_relationship_edges`,
`list_relationship_edges`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: whole_graph`, `p95_latency_ms: 3000`,
`max_truth_level: exact`.

## Claim validated

Fixed typed-edge verb catalog, each verb counted with one anonymous
relationship-type-indexed whole-graph aggregate, plus a bounded
source-label-anchored per-verb concrete edge slice with `LIMIT`, including
scoped-token cross-tenant edge hiding and source-tool filtering.

## Committed reproducible evidence

**Verb tiles, bounded edges, unknown-verb/tool rejection, source-tool
breakdown** — `go/internal/query/relationships_catalog_test.go`:
`TestGetRelationshipsCatalogReturnsVerbTiles`,
`TestGetRelationshipEdgesReturnsBoundedSlice`,
`TestGetRelationshipEdgesRejectsUnknownVerb`,
`TestGetRelationshipEdgesRequiresVerb`,
`TestGetRelationshipEdgesFiltersBySourceTool`,
`TestGetRelationshipEdgesRejectsUnknownSourceTool`,
`TestGetRelationshipsCatalogIncludesSourceToolsBreakdown`,
`TestRelationshipCountCypherIsTypeIndexed`,
`TestRelationshipEdgesCypherIsSourceAnchoredAndIndexOrdered`. Reproduce:

```bash
cd go && go test ./internal/query -run 'TestGetRelationshipsCatalog|TestGetRelationshipEdges|TestRelationship(Count|Edges)Cypher' -count=1
```

**Scoped-token cross-tenant enforcement** —
`go/internal/query/relationships_catalog_scoped_test.go`:
`TestGetRelationshipEdgesScopedEmptyGrantReturnsEmptyWithoutGraphRead`,
`TestGetRelationshipEdgesScopedGrantBindsBothEndpointsAndReturnsRealRowData`,
`TestGetRelationshipEdgesScopedGrantHidesCrossTenantTargetEdge`,
`TestGetRelationshipEdgesScopedGrantExcludesSharedWorkloadCollisionLeak`.

**Scoped-token grant filtering and performance/observability evidence** —
`docs/internal/evidence/5167-w6-scoped-cloud-routes.md` (this route is
explicitly named as one of the three hot-path Cypher files touched by #5167
W6).

## Notes

No private data: this artifact cites only committed tests and a committed
evidence note, no deployment-specific values.

Related: #5552 (burn-down), #5407 (artifact-existence gate).
