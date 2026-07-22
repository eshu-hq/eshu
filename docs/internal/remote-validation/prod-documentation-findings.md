# prod-documentation-findings — production validation

Capability: `documentation_findings.list` (tool `list_documentation_findings`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: multi_repo_platform`, `p95_latency_ms: 1500`,
`max_truth_level: exact`.

## Claim validated

`documentation_finding` facts are served by filter (scope, status,
`updated_since`, etc.) with input validation and stable error behavior for
unsupported profiles or an unavailable read model.

## Committed reproducible evidence

**Handler contract, filter validation, and stable error states** —
`go/internal/query/documentation_test.go`:
`TestDocumentationHandlerListsFindings`,
`TestDocumentationHandlerRejectsInvalidUpdatedSince`,
`TestDocumentationHandlerReportsUnsupportedProfileWithStableError`, and
`TestDocumentationHandlerReportsUnavailableReadModelWithStableError`.
Reproduce:

```bash
cd go && go test ./internal/query -run TestDocumentationHandlerListsFindings -count=1
```

**Content-store filtering** —
`go/internal/query/documentation_test.go`:
`TestContentReaderDocumentationFindingsFiltersAndBuildsPacketURL`.

## Notes

No private data: cited tests use synthetic documentation-finding fixtures; no
production credentials or deployment-specific values appear in this
artifact.

Related: #5407 (artifact-existence gate), #5552 (burn-down).
