# prod-call-graph-metrics — production validation

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
