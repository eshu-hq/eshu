# prod-container-image-identity-aggregate — production validation

Capability: `supply_chain.container_image_identities.aggregate` (tools
`count_container_image_identities`, `get_container_image_identity_inventory`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: optional_digest_ref_repository_or_outcome_scope`, `p95_latency_ms: 1500`,
`max_truth_level: exact`.

## Claim validated

Bounded reducer container image identity aggregate — count and grouped inventory by outcome,
`identity_strength`, or `repository_id` — replacing a page-and-iterate caller workflow for
ecosystem totals questions.

## Committed reproducible evidence

**Count and grouped-inventory rollups** — `go/internal/query/container_image_identity_aggregates_test.go`:
`TestContainerImageIdentityAggregateCountReturnsRollups`,
`TestContainerImageIdentityAggregateInventoryReturnsBuckets`,
`TestContainerImageIdentityAggregateRoutesForwardSourceRepositoryScope`,
`TestContainerImageIdentityAggregateInventoryReportsTruncated`, and
`TestContainerImageIdentityAggregateQueriesUseSourceRepositoryAnchor`. Reproduce:

```bash
cd go && go test ./internal/query -run TestContainerImageIdentityAggregate -count=1
```

**Input validation and pagination bound closure** — same file:
`TestContainerImageIdentityAggregateInventoryRejectsUnknownDimension`,
`TestContainerImageIdentityAggregateInventoryRejectsOversizedLimit`,
`TestContainerImageIdentityAggregateInventoryRejectsNegativeOffset`,
`TestContainerImageIdentityAggregateRejectsUnknownOutcome`,
`TestContainerImageIdentityAggregateInventoryRejectsOversizedOffset`,
`TestContainerImageIdentityAggregateInventoryNullsNextOffsetAtCeiling`,
`TestNextContainerImageIdentityAggregateOffsetBound`, and
`TestContainerImageIdentityInventoryGroupExpressionEnumIsClosed`. Reproduce:

```bash
cd go && go test ./internal/query -run "TestContainerImageIdentityAggregate.*Rejects|TestNextContainerImageIdentityAggregateOffsetBound|TestContainerImageIdentityInventoryGroupExpressionEnumIsClosed" -count=1
```

**Store availability guard** — same file:
`TestContainerImageIdentityAggregateRoutesReturn503WhenStoreMissing`. Reproduce:

```bash
cd go && go test ./internal/query -run TestContainerImageIdentityAggregateRoutesReturn503WhenStoreMissing -count=1
```

**Missing-evidence classification** — `go/internal/query/container_image_identity_aggregate_missing_evidence_test.go`:
`TestContainerImageIdentityAggregateCountReportsSourceBridgeMissingEvidence`. Reproduce:

```bash
cd go && go test ./internal/query -run TestContainerImageIdentityAggregateCountReportsSourceBridgeMissingEvidence -count=1
```

## Notes

No private data: aggregate fixtures use synthetic image digests, refs, and repository IDs only.

Related: #5552 (burn-down).
