# prod-class-methods — production validation

Validation-Slug: prod-class-methods
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: symbol_graph.class_methods passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


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
