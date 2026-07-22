# prod-call-chain-path — production validation

Capability: `call_graph.call_chain_path` (tool `find_function_call_chain`).
Production profile: `required_runtime: deployed_services`, `max_scope_size: multi_repo_platform`,
`p95_latency_ms: 5000`, `max_truth_level: exact`.

## Claim validated

Authoritative shortest-path query between two functions over the CALLS graph, supporting
entity-ID and repo-scoped name lookups on both Neo4j and NornicDB backends.

## Committed reproducible evidence

**Shortest-path resolution across backends** — `go/internal/query/code_call_graph_contract_test.go`:
`TestHandleCallChainReturnsShortestPath`, `TestHandleCallChainUsesNornicDBBFSForNameAnchors`,
`TestHandleCallChainSupportsEntityIDAndRepoScopedLookup`, and
`TestHandleCallChainSupportsEntityIDAndRepoScopedLookupForNornicDB`. Reproduce:

```bash
cd go && go test ./internal/query -run TestHandleCallChain -count=1
```

**Cross-repository endpoint selectors and repo-scoped filtering** —
`go/internal/query/code_call_chain_cross_repo_test.go`:
`TestBuildCallChainCypherCrossRepoUsesEndpointRepositorySelectors`,
`TestBuildCallChainCypherRepoScopedFiltersEveryPathNode`, and
`TestHandleCallChainRejectsCrossRepoEndpointSelectorOutsideGrant`. Reproduce:

```bash
cd go && go test ./internal/query -run TestBuildCallChainCypher -count=1
cd go && go test ./internal/query -run TestHandleCallChainRejectsCrossRepo -count=1
```

**Repository selector aliasing and ambiguity resolution** —
`go/internal/query/code_repository_selector_test.go`:
`TestHandleCallChainResolvesRepositorySelectorAlias`, and
`go/internal/query/entity_resolution_test.go`:
`TestHandleCallChainResolvesRepoScopedNamesToNonTestEntityIDs`,
`TestHandleCallChainDisambiguatesRepoScopedNamesByReachability`. Reproduce:

```bash
cd go && go test ./internal/query -run "TestHandleCallChainResolvesRepositorySelectorAlias|TestHandleCallChainResolvesRepoScopedNames|TestHandleCallChainDisambiguates" -count=1
```

**Local-lightweight unsupported-capability guard** — `go/internal/query/contract_endpoint_test.go`:
`TestHandleCallChain_LocalLightweightReturnsStructuredUnsupportedCapability`. Reproduce:

```bash
cd go && go test ./internal/query -run TestHandleCallChain_LocalLightweightReturnsStructuredUnsupportedCapability -count=1
```

## Notes

No private data: all fixtures use synthetic repository IDs and function names.

Related: #5552 (burn-down).
