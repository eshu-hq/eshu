# prod-security-alert-reconciliation-aggregate — production validation

Capability: `supply_chain.security_alert_reconciliations.aggregate` (tools
`count_security_alert_reconciliations`,
`get_security_alert_reconciliation_inventory`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: optional_repository_provider_package_or_advisory_scope`,
`p95_latency_ms: 1500`, `max_truth_level: exact`.

## Claim validated

Bounded reducer alert aggregate returning grouped counts by
`reconciliation_status`, `provider`, `provider_state`, `repository_id`, or
`package_id`, replacing a page-and-iterate caller workflow for ecosystem-totals
questions.

## Committed reproducible evidence

**Aggregate rollup contract, bucket dimensions, and truncation** —
`go/internal/query/security_alert_reconciliation_aggregates_test.go`:
`TestSecurityAlertReconciliationAggregateRoutesReturn503WhenStoreMissing`,
`TestSecurityAlertReconciliationAggregateCountReturnsRollups`,
`TestSecurityAlertReconciliationAggregateInventoryReturnsBuckets`,
`TestSecurityAlertReconciliationAggregateInventoryReportsTruncated`,
`TestSecurityAlertReconciliationAggregateInventoryRejectsUnknownDimension`, and
`TestSecurityAlertReconciliationAggregateInventoryRejectsOversizedLimit`.
Reproduce:

```bash
cd go && go test ./internal/query -run TestSecurityAlertReconciliationAggregate -count=1
```

## Notes

No private data: aggregate rows carry counts and bucket labels only, never raw
provider alert payloads.

Related: #5552 (burn-down).
