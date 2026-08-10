# prod-relationship-evidence — production validation

Validation-Slug: prod-relationship-evidence
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: relationship_evidence.drilldown passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.
B12-Assertion: relationship_evidence.drilldown -> mcp:get_repo_context

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


Capability: `relationship_evidence.drilldown` (tools `get_repo_context`,
`get_relationship_evidence`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: multi_repo_platform`, `p95_latency_ms: 1500`,
`max_truth_level: exact`. `resolved_relationships` drilldown by
`resolved_id`.

## Claim validated

Resolved-relationship drilldown by `resolved_id` returns real row data for
in-grant callers, `404` for missing rows, and enforces scoped-token
endpoint-grant checks (including cross-tenant denial and empty-grant
short-circuiting before any store read).

## Committed reproducible evidence

**Handler behavior and content-index hydration** —
`go/internal/query/evidence_test.go`:
`TestEvidenceHandlerReturnsRelationshipEvidenceByResolvedID`,
`TestEvidenceHandlerReturnsNotFoundForMissingRelationshipEvidence`,
`TestContentReaderRelationshipEvidenceByResolvedIDHydratesDetails`.
Reproduce:

```bash
cd go && go test ./internal/query -run TestEvidenceHandlerReturnsRelationshipEvidence -count=1
cd go && go test ./internal/query -run TestContentReaderRelationshipEvidence -count=1
```

**Scoped-token grant enforcement** —
`go/internal/query/evidence_scoped_test.go`:
`TestEvidenceHandlerScopedTokenWithBothEndpointsGrantedReturnsRealRowData`,
`TestEvidenceHandlerScopedTokenMissingTargetGrantReturnsNotFound`,
`TestEvidenceHandlerScopedTokenSourceOwnerReachesGlobalTargetEvidence`,
`TestEvidenceHandlerScopedTokenNonSourceOwnerDeniedGlobalTargetEvidence`,
`TestEvidenceHandlerScopedTokenEmptyGrantReturnsNotFound`. Reproduce:

```bash
cd go && go test ./internal/query -run TestEvidenceHandlerScopedToken -count=1
```

## Notes

No private data: this artifact cites only committed tests, no
deployment-specific values.

Related: #5552 (burn-down), #5407 (artifact-existence gate).
