# prod-call-graph-metrics — production validation

Validation-Slug: prod-call-graph-metrics
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: call_graph.metrics passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.
B12-Assertion: call_graph.metrics -> mcp:inspect_call_graph_metrics

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


Capability: `call_graph.metrics` (tool `inspect_call_graph_metrics`).
Production profile: `required_runtime: deployed_services`, `max_scope_size: multi_repo_platform`,
`p95_latency_ms: 5000`, `max_truth_level: exact`.

## Claim validated

Repo-anchored graph metrics for recursive and high-degree functions with bounded, deterministic
pages, computed from one indexed pass over the repository's directed `CALLS` edges.

## Committed reproducible evidence

**Handler-level bounded hub/recursive metrics** — `go/internal/query/code_call_graph_metrics_test.go`:
`TestHandleCallGraphMetricsReturnsBoundedHubFunctions`,
`TestHandleCallGraphMetricsReturnsRecursiveFunctions`,
`TestHandleCallGraphMetricsRejectsUnscopedRequests`,
`TestHandleCallGraphMetricsFailsClosedWhenEdgeScanLimitExceeded`, and
`TestCallGraphMetricsResponseUsesGlobalRankAndCapsNextOffset`. Reproduce:

```bash
cd go && go test ./internal/query -run TestHandleCallGraphMetrics -count=1
cd go && go test ./internal/query -run TestCallGraphMetricsResponseUsesGlobalRankAndCapsNextOffset -count=1
```

**One-pass edge aggregation correctness** — `go/internal/query/code_call_graph_metrics_aggregation_test.go`:
`TestCallGraphMetricsEdgesCypherUsesOneRepoIndexedEdgePass`,
`TestCallGraphMetricsRowsAggregatesHubFunctionsExactly`,
`TestCallGraphMetricsRowsKeepsCanonicalUIDsDistinctWhenLegacyIDsCollide`,
`TestCallGraphMetricsRowsUsesCanonicalUIDsForRecursivePairs`, and
`TestCallGraphMetricsDataFailsClosedAndRecordsScanOverflow`. Reproduce:

```bash
cd go && go test ./internal/query -run TestCallGraphMetrics -count=1
```

**Performance evidence record** — `docs/internal/evidence/5564-call-graph-metrics.md` documents the
one-pass rewrite (#5564) that replaced two repeated-expansion query shapes exceeding a 75-second
deadline on 42,197 `CALLS` relationships with a single indexed-edge read.

**Contract declaration** — `go/internal/query/openapi_call_graph_metrics_test.go`:
`TestOpenAPICallGraphMetrics`. Reproduce:

```bash
cd go && go test ./internal/query -run TestOpenAPICallGraphMetrics -count=1
```

## Notes

No private data: all fixtures use synthetic repository IDs and function edge counts; the
evidence doc reports timing figures only, no hostnames or credentials.

Related: #5552 (burn-down).
