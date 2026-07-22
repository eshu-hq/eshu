# prod-graph-summary-packet — production validation

Capability: `platform_impact.graph_summary_packet` (tool
`get_graph_summary_packet`). Production profile:
`required_runtime: deployed_services`, `max_scope_size: optional_repo_scope`,
`p95_latency_ms: 3000`, `max_truth_level: exact`.

## Claim validated

A bounded summary packet reusing the repo-anchored hub-function degree shape
(`LIMIT` on hot entities), per-type repo-anchored relationship counts, and
per-label/repo-anchored ecosystem counts; without `repo_id` only the bounded
per-label ecosystem counts are returned with an explicit needs-repo note
rather than an unscoped whole-graph scan.

## Committed reproducible evidence

**Handler contract, scoped-vs-unscoped shape, and truth envelope** —
`go/internal/query/infra_graph_summary_packet_test.go`:
`TestGraphSummaryPacketRepoScopedShapeIsBoundedAndDeterministic`,
`TestGraphSummaryPacketWithoutRepoReturnsEcosystemCountsAndNote`,
`TestGraphSummaryPacketEmptyGraphReturnsZerosNotError`,
`TestGraphSummaryPacketHonorsLimitTruncation`, and
`TestGraphSummaryPacketTruthEnvelopePresent`. Reproduce:

```bash
cd go && go test ./internal/query -run TestGraphSummaryPacket -count=1
```

**Scoped-grant authorization** —
`go/internal/query/infra_graph_summary_packet_test.go`:
`TestGraphSummaryPacketRepoScopedOutOfGrantReturnsNotFound` and
`TestGraphSummaryPacketRepoScopedInGrantReturnsRealRowData`.

## Notes

No private data: cited tests use synthetic graph fixtures; no production
credentials or deployment-specific values appear in this artifact.

Related: #5407 (artifact-existence gate), #5552 (burn-down).
