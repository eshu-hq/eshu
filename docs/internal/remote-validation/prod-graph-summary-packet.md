# prod-graph-summary-packet — production validation

Validation-Slug: prod-graph-summary-packet
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: platform_impact.graph_summary_packet passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.
B12-Assertion: platform_impact.graph_summary_packet -> mcp:get_graph_summary_packet

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


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
