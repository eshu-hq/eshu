# prod-direct-callees — production validation

Capability: `call_graph.direct_callees` (tool `analyze_code_relationships`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: multi_repo_platform`, `p95_latency_ms: 3000`,
`max_truth_level: exact`.

## Claim validated

Authoritative `CALLS`-edge direct-callee reads served by the graph sidecar,
including NornicDB-specific row-query and indexed-fallback shapes.

## Committed reproducible evidence

**Direct-call graph query contract** —
`go/internal/query/code_relationships_nornicdb_test.go`:
`TestHandleRelationshipsUsesNornicDBRowQueriesForDirectCalls` and
`TestHandleRelationshipsUsesIndexedFallbackForNornicDBDirectCalls`.
Reproduce:

```bash
cd go && go test ./internal/query -run TestHandleRelationships.*DirectCalls -count=1
```

**Full-stack Docker Compose route parity** —
`scripts/verify_graph_analysis_dogfood_compose.sh` captures a live
direct-callees response (`DIRECT_CALLEES_FILE`) against a Compose stack.
Reproduce (requires Docker Compose):

```bash
scripts/verify_graph_analysis_dogfood_compose.sh
```

## Notes

No private data: cited tests and the Compose fixture use synthetic
repositories; no production credentials or deployment-specific values appear
in this artifact.

Related: #5407 (artifact-existence gate), #5552 (burn-down).
