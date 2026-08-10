# prod-container-image-identity-aggregate — production validation

Validation-Slug: prod-container-image-identity-aggregate
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: supply_chain.container_image_identities.aggregate passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.
B12-Assertion: supply_chain.container_image_identities.aggregate -> mcp:count_container_image_identities

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


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
