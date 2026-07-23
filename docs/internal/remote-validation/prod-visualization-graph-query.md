# prod-visualization-graph-query — production validation

Capability: `visualization.graph_query` (tool `visualize_graph_query`,
route `POST /api/v0/code/visualize`, handler
`(h *CodeHandler) handleVisualizeQuery`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: bounded_query_window`, `p95_latency_ms: 2000`,
`max_truth_level: exact`.

## Claim validated

Bounds and executes caller-supplied read-only Cypher, then projects the
returned graph entities (nodes, relationships, paths) into a bounded
node/edge visualization packet.

## Committed reproducible evidence

**Bounded execution, empty results, mutation rejection, and capability
envelope** — `go/internal/query/code_cypher_test.go`:
`TestHandleVisualizeQuery_ReturnsGraphPacket` (asserts the handler returns a
visualization packet with nodes/edges derived from the query result, not a
hardcoded browser URL),
`TestHandleVisualizeQuery_EmptyResult`,
`TestHandleVisualizeQuery_RejectsMutations` (proves write/mutation Cypher is
refused),
`TestHandleVisualizeQuery_ErrorEnvelopeCarriesVisualizationCapability`,
`TestHandleVisualizeQuery_InnerLimitGetsTerminalCap` (proves the injected
`LIMIT` caps the query regardless of caller-supplied limits), and
`TestBoundedVisualizationCypher_TerminalCap`. Reproduce:

```bash
cd go && go test ./internal/query -run 'TestHandleVisualizeQuery_|TestBoundedVisualizationCypher_' -count=1
```

## Notes

No private data: cited tests execute against fixture graph fakes only, never a
live deployment's data.

Related: #5552 (burn-down).
