# prod-visualization-graph-query — production validation

Validation-Slug: prod-visualization-graph-query
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: visualization.graph_query passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


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
