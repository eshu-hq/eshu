# prod-relationship-story — production validation

Validation-Slug: prod-relationship-story
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: call_graph.relationship_story passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


Capability: `call_graph.relationship_story` (tool
`get_code_relationship_story`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: multi_repo_platform`, `p95_latency_ms: 3000`,
`max_truth_level: exact`.

## Claim validated

Bounded graph relationship story anchored by resolved entity id: class
hierarchy, overrides, one-hop and bounded transitive `CALLS` traversal,
cross-repo grant enforcement, edge provenance, and a confidence floor, all
without guessing ambiguous candidates.

## Committed reproducible evidence

**Class hierarchy, overrides, cross-repo enforcement, provenance, transitive
bounds** — `go/internal/query/code_relationship_story_test.go` and
`go/internal/query/code_relationship_story_class_test.go`:
`TestHandleRelationshipStoryReturnsClassHierarchyPacket`,
`TestHandleRelationshipStoryListsOverridesWithoutTarget`,
`TestHandleRelationshipStoryRejectsCrossRepoRepositoryOutsideGrant`,
`TestHandleRelationshipStorySurfacesEdgeProvenance`,
`TestHandleRelationshipStoryAppliesMinConfidenceFloor`,
`TestHandleRelationshipStoryReturnsAmbiguousCandidatesWithoutGuessing`,
`TestHandleRelationshipStoryUsesBoundedGraphQuery`,
`TestHandleRelationshipStoryTraversesTransitiveCallsWithDepthLimit`,
`TestHandleRelationshipStoryRejectsTransitiveOffset`. Reproduce:

```bash
cd go && go test ./internal/query -run TestHandleRelationshipStory -count=1
```

**OpenAPI contract declaration** —
`go/internal/query/openapi_relationship_story_test.go`.

## Notes

No private data: this artifact cites only committed tests, no
deployment-specific values.

Related: #5552 (burn-down), #5407 (artifact-existence gate).
