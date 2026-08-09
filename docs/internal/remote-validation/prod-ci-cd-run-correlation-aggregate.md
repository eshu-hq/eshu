# prod-ci-cd-run-correlation-aggregate — production validation

Validation-Slug: prod-ci-cd-run-correlation-aggregate
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: ci_cd.run_correlations.aggregate passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.
B12-Assertion: ci_cd.run_correlations.aggregate -> mcp:count_ci_cd_run_correlations

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


Capability: `ci_cd.run_correlations.aggregate` (tools `count_ci_cd_run_correlations`,
`get_ci_cd_run_correlation_inventory`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: optional_run_or_commit_or_artifact_scope`, `p95_latency_ms: 1500`,
`max_truth_level: exact`.

## Claim validated

Bounded reducer CI/CD run correlation aggregate — count and grouped inventory by outcome,
environment, repository_id, or provider — replacing a page-and-iterate caller workflow for
ecosystem totals questions.

## Committed reproducible evidence

**Count and grouped-inventory rollups** — `go/internal/query/ci_cd_run_correlation_aggregates_test.go`:
`TestCICDRunCorrelationAggregateCountReturnsRollups`,
`TestCICDRunCorrelationAggregateCountPassesImageRefFilter`,
`TestCICDRunCorrelationAggregateInventoryReturnsBuckets`,
`TestCICDRunCorrelationAggregateInventoryPassesImageRefFilter`, and
`TestCICDRunCorrelationAggregateInventoryReportsTruncated`. Reproduce:

```bash
cd go && go test ./internal/query -run TestCICDRunCorrelationAggregate -count=1
```

**Input validation (dimension, limit, offset)** — same file:
`TestCICDRunCorrelationAggregateRejectsUnknownOutcome`,
`TestCICDRunCorrelationAggregateInventoryRejectsUnknownDimension`,
`TestCICDRunCorrelationAggregateInventoryRejectsOversizedLimit`,
`TestCICDRunCorrelationAggregateInventoryRejectsNegativeOffset`, and
`TestCICDRunCorrelationAggregateInventoryRejectsOversizedOffset`. Reproduce:

```bash
cd go && go test ./internal/query -run "TestCICDRunCorrelationAggregate.*Rejects" -count=1
```

**Store availability and pagination bound closure** — same file:
`TestCICDRunCorrelationAggregateRoutesReturn503WhenStoreMissing`,
`TestCICDRunCorrelationAggregateInventoryNullsNextOffsetAtCeiling`,
`TestNextCICDRunCorrelationAggregateOffsetBound`, and
`TestCICDRunCorrelationInventoryGroupExpressionEnumIsClosed`. Reproduce:

```bash
cd go && go test ./internal/query -run TestCICDRunCorrelationAggregateRoutesReturn503WhenStoreMissing -count=1
cd go && go test ./internal/query -run "TestNextCICDRunCorrelationAggregateOffsetBound|TestCICDRunCorrelationInventoryGroupExpressionEnumIsClosed" -count=1
```

**Repository-selector resolution** — `go/internal/query/repository_selector_read_model_routes_test.go`:
`TestCICDRunCorrelationAggregatesResolveRepositorySelectors`. Reproduce:

```bash
cd go && go test ./internal/query -run TestCICDRunCorrelationAggregatesResolveRepositorySelectors -count=1
```

## Notes

No private data: aggregate fixtures use synthetic run/commit/artifact identifiers only.

Related: #5552 (burn-down).
