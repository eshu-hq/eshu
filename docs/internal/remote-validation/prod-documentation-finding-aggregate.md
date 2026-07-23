# prod-documentation-finding-aggregate — production validation

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
