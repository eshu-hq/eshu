# prod-direct-callees — production validation

Validation-Slug: prod-direct-callees
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: call_graph.direct_callees passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


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
