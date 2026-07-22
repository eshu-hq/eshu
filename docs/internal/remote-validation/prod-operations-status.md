# prod-operations-status — production validation

Capability: `operations.status` (route `GET /api/v0/status/operations`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: bounded_live_operations`, `p95_latency_ms: 2000`,
`max_truth_level: exact`.

## Claim validated

All-scopes callers receive exact deployed operations status (health,
collectors, queue, live activity); scoped callers receive derived
grant-filtered live activity only, with process-global aggregates and
identities withheld — no cross-tenant leakage.

## Committed reproducible evidence

**Composition, scoped withholding, and identity redaction** —
`go/internal/query/status_operations_test.go`:
`TestGetOperationsComposesHealthCollectorsQueueAndLiveActivity`,
`TestGetOperationsScopedWithGrantsSeesOnlyGrantedRowsIdentityRedacted`,
`TestGetOperationsScopedWithNoGrantsSeesZeroRowsWithoutQuerying`,
`TestGetOperationsLimitValidation`,
`TestGetOperationsEmptyLiveActivity`,
`TestGetOperationsLiveActivityReaderError`,
`TestGetOperationsStatusReaderUnavailable`,
`TestGetOperationsLiveActivityReaderUnavailable`. Reproduce:

```bash
cd go && go test ./internal/query -run TestGetOperations -count=1
```

**Envelope truth-level negotiation and scoped aggregate withholding** —
`go/internal/query/status_operations_envelope_test.go`:
`TestGetOperationsNegotiatesEnvelopeAndPreservesLegacyRawJSON`,
`TestGetOperationsScopedCallerWithholdsGlobalAggregatesAndDowngradesTruth`.

**Scope-truth agreement (exact vs derived by grant)** —
`go/internal/query/status_operations_scope_truth_test.go`.

## Notes

No private data: this artifact cites only committed tests, no
deployment-specific values.

Related: #5552 (burn-down), #5407 (artifact-existence gate).
