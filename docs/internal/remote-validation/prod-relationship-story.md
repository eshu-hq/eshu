# prod-relationship-story — production validation

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
