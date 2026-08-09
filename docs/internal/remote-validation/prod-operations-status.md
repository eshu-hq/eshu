# prod-operations-status — production validation

Validation-Slug: prod-operations-status
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: operations.status passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


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
