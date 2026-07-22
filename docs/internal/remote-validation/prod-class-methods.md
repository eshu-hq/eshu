# prod-class-methods — production validation

Capability: `symbol_graph.class_methods` (tools `execute_language_query`,
`analyze_code_relationships`).
Production profile: `required_runtime: deployed_services`, `max_scope_size: multi_repo_platform`,
`p95_latency_ms: 1200`, `max_truth_level: exact`.

## Claim validated

Durable semantic facts: class method listings served through the graph relationship-story class
hierarchy packet (`analyze_code_relationships` / `get_code_relationship_story`), anchored by
resolved class entity ID.

## Committed reproducible evidence

**Class hierarchy packet including methods** — `go/internal/query/code_relationship_story_class_test.go`:
`TestHandleRelationshipStoryReturnsClassHierarchyPacket` (exercises `relationshipStoryClassMethods`
as part of the class hierarchy response) and
`TestHandleRelationshipStoryClassHierarchyFiltersTargetResolutionToClasses`. Reproduce:

```bash
cd go && go test ./internal/query -run TestHandleRelationshipStoryClassHierarchy -count=1
cd go && go test ./internal/query -run TestHandleRelationshipStoryReturnsClassHierarchyPacket -count=1
```

**NornicDB Cypher shape for class methods** — same file:
`TestNornicDBRelationshipStoryClassMethodsCypherUsesAnchoredClassPattern`. Reproduce:

```bash
cd go && go test ./internal/query -run TestNornicDBRelationshipStoryClassMethodsCypherUsesAnchoredClassPattern -count=1
```

**Non-class entity rejection** — same file:
`TestHandleRelationshipStoryClassHierarchyRejectsNonClassEntityID`. Reproduce:

```bash
cd go && go test ./internal/query -run TestHandleRelationshipStoryClassHierarchyRejectsNonClassEntityID -count=1
```

## Notes

The `execute_language_query` side of this capability's tool list rides the same generic
entity-type-parametrized dispatch covered by `go/internal/query/language_queries_test.go`
(`TestBuildLanguageCypher_AllEntityTypes`); the class-methods-specific behavior asserted above is
served through `analyze_code_relationships`.

No private data: fixtures use synthetic class/method entity IDs only.

Related: #5552 (burn-down).
