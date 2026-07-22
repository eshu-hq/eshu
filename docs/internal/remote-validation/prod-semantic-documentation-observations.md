# prod-semantic-documentation-observations — production validation

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
