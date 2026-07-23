# prod-relationship-evidence — production validation

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
