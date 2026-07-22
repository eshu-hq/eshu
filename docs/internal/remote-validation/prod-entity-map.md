# prod-entity-map — production validation

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
