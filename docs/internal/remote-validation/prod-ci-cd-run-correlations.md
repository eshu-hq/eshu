# prod-ci-cd-run-correlations — production validation

Capability: `ci_cd.run_correlations.list` (tool `list_ci_cd_run_correlations`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: run_or_commit_or_artifact_scope`, `p95_latency_ms: 1500`,
`max_truth_level: exact`.

## Claim validated

Bounded reducer CI/CD run correlation lookup anchored by scope, repository, commit, provider
plus provider run for run-only reads, artifact digest, or environment; repository-scoped list
responses also summarize static workflow artifacts from the content read model without creating
synthetic correlation rows.

## Committed reproducible evidence

**Scope/limit validation and bounded Postgres store lookup** — `go/internal/query/ci_cd_run_correlations_test.go`:
`TestCICDListRunCorrelationsRequiresScopeAndLimit`,
`TestCICDListRunCorrelationsUsesBoundedPostgresStore`,
`TestCICDListRunCorrelationsUsesImageRefAnchor`,
`TestCICDListRunCorrelationsRequiresProviderForProviderRunID`, and
`TestCICDListRunCorrelationsPassesProviderRunDisambiguator`. Reproduce:

```bash
cd go && go test ./internal/query -run TestCICDListRunCorrelations -count=1
```

**Static workflow artifact summary without synthetic rows** — same file:
`TestCICDListRunCorrelationsHydratesStaticWorkflowArtifactsOnce`,
`TestCICDListRunCorrelationsExplainsStaticWorkflowOnlyEvidence`,
`TestCICDListRunCorrelationsExplainsLiveRunEvidence`, and
`TestCICDListRunCorrelationsExplainsNoEvidence`; artifact-digest evidence detail in
`go/internal/query/ci_cd_evidence_summary_artifact_test.go`:
`TestCICDListRunCorrelationsExplainsWorkflowArtifactDigestEvidence` and
`TestCICDListRunCorrelationsExplainsAmbiguousArtifactEvidence`. Reproduce:

```bash
cd go && go test ./internal/query -run "TestCICDListRunCorrelationsHydrates|TestCICDListRunCorrelationsExplains" -count=1
```

**Scoped-token authorization** — `go/internal/query/ci_cd_authz_test.go`:
`TestAuthMiddlewareWithScopedTokensAllowsCICDRunCorrelationRoutes`,
`TestCICDRunCorrelationScopedEmptyGrantReturnsEmptyWithoutStoreRead`,
`TestCICDRunCorrelationScopedRepositorySelectorDeniesOutOfGrantWithoutStoreRead`, and
`TestCICDRunCorrelationSQLAppliesScopedAuthorizationBeforeOrderAndGrouping`. Reproduce:

```bash
cd go && go test ./internal/query -run TestCICDRunCorrelation -count=1
```

**Repository-selector resolution and contract declaration** —
`go/internal/query/repository_selector_read_model_routes_test.go`:
`TestCICDRunCorrelationsResolveRepositorySelectors`, and
`go/internal/query/openapi_cicd_test.go`: `TestOpenAPISpecIncludesCICDRunCorrelations`. Reproduce:

```bash
cd go && go test ./internal/query -run "TestCICDRunCorrelationsResolveRepositorySelectors|TestOpenAPISpecIncludesCICDRunCorrelations" -count=1
```

## Notes

No private data: fixtures use synthetic run/commit/provider identifiers only.

Related: #5552 (burn-down).
