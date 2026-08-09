# prod-ci-cd-run-correlations — production validation

Validation-Slug: prod-ci-cd-run-correlations
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: ci_cd.run_correlations.list passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.
B12-Assertion: ci_cd.run_correlations.list -> mcp:list_ci_cd_run_correlations

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


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
