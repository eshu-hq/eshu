# prod-semantic-documentation-observations — production validation

Validation-Slug: prod-semantic-documentation-observations
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: semantic_evidence.documentation_observations.list passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


Capability: `semantic_evidence.documentation_observations.list` (tool
`list_semantic_documentation_observations`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: multi_repo_platform`, `p95_latency_ms: 1500`,
`max_truth_level: derived`.

## Claim validated

Bounded semantic documentation observation facts by source, document,
repository, service, fact, provider, freshness, status, policy, or observation
scope, with redacted provenance and no raw prompt payloads or private provider
responses.

## Committed reproducible evidence

**Handler-level listing with truth metadata and scope enforcement** —
`go/internal/query/semantic_evidence_test.go`:
`TestSemanticEvidenceHandlerListsDocumentationObservationsWithTruthMetadata`,
`TestSemanticEvidenceHandlerScopedEmptyGrantReturnsEmptyWithoutRead`,
`TestSemanticEvidenceHandlerAllScopeScopedAdminKeepsUnboundedSemanticFilter`,
`TestBuildSemanticEvidenceSQLAppliesScopedRepositoryAuthorizationBeforePaging`,
`TestSemanticEvidencePublicRowDropsProviderInternals`,
`TestSemanticEvidencePublicRowSurfacesBoundedSourceACLState`, and
`TestSemanticEvidencePublicRowOmitsAbsentSourceACLState`. Reproduce:

```bash
cd go && go test ./internal/query -run 'TestSemanticEvidenceHandler|TestBuildSemanticEvidenceSQLAppliesScopedRepositoryAuthorizationBeforePaging|TestSemanticEvidencePublicRow' -count=1
```

## Notes

No private data: cited tests assert redaction of provider internals, no raw
prompt payloads, and scope-bounded reads.

Related: #5552 (burn-down).
