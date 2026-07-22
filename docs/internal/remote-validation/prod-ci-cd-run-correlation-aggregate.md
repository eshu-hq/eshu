# prod-ci-cd-run-correlation-aggregate — production validation

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
