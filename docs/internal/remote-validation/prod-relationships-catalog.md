# prod-relationships-catalog — production validation

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
