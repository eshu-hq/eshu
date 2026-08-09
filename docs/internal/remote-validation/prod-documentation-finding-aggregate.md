# prod-documentation-finding-aggregate — production validation

Validation-Slug: prod-documentation-finding-aggregate
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: documentation_findings.aggregate passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.
B12-Assertion: documentation_findings.aggregate -> mcp:count_documentation_findings

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


Capability: `documentation_findings.aggregate` (tools
`count_documentation_findings`, `get_documentation_finding_inventory`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: optional_scope_finding_type_source_document_or_status_scope`,
`p95_latency_ms: 1500`, `max_truth_level: exact`.

## Claim validated

Bounded documentation finding aggregate (count and grouped inventory by
`status`, `truth_level`, `freshness_state`, `finding_type`, or `source_id`)
over permission-gated reducer facts, replacing a page-and-iterate caller
workflow for ecosystem-totals questions.

## Committed reproducible evidence

**Handler contract, grouping dimensions, and bounds validation** —
`go/internal/query/documentation_finding_aggregates_test.go`:
`TestDocumentationFindingAggregateCountReturnsRollups`,
`TestDocumentationFindingAggregateInventoryReturnsBuckets`,
`TestDocumentationFindingAggregateInventoryReportsTruncated`,
`TestDocumentationFindingAggregateInventoryRejectsUnknownDimension`, and
`TestDocumentationFindingAggregateInventoryRejectsOversizedLimit`. Reproduce:

```bash
cd go && go test ./internal/query -run TestDocumentationFindingAggregate -count=1
```

**Store-unavailable honesty** —
`go/internal/query/documentation_finding_aggregates_test.go`:
`TestDocumentationFindingAggregateRoutesReturn503WhenStoreMissing`.

## Notes

No private data: cited tests use synthetic documentation-finding fixtures; no
production credentials or deployment-specific values appear in this
artifact.

Related: #5407 (artifact-existence gate), #5552 (burn-down).
