# prod-import-dependencies — production validation

Validation-Slug: prod-import-dependencies
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: symbol_graph.import_dependencies passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


Capability: `symbol_graph.import_dependencies` (tool `investigate_import_dependencies`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: multi_repo_platform`, `p95_latency_ms: 1500`,
`max_truth_level: exact`.

## Claim validated

Bounded graph import and cross-module relationship reads: per-file imports,
file-import-cycle detection, and repository/language identity are preserved
exactly (not guessed) across the request's scope.

## Committed reproducible evidence

**Handler behavior and bounds** — `go/internal/query/code_import_dependencies_test.go`:
`TestHandleImportDependencyInvestigationReturnsBoundedImportsByFile`,
`TestHandleImportDependencyInvestigationReturnsFileImportCycles`,
`TestHandleImportDependencyInvestigationTruncatesFileImportCycles`,
`TestHandleImportDependencyInvestigationReportsUnavailableCycleBackend`.
Reproduce:

```bash
cd go && go test ./internal/query -run TestHandleImportDependencyInvestigation -count=1
```

**Exactness of module/repository/language identity** —
`go/internal/query/code_import_dependencies_exactness_test.go`:
`TestImportDependencyUniqueModulesPreservesRepositoryAndLanguageIdentity`,
`TestUniqueImportDependencyScopesPreservesRepositoryPathIdentity`,
`TestBuildFileImportCycleRowsUsesExactDottedModuleNames`,
`TestHandleImportDependencyInvestigationFiltersRepositoryPathCollisions`,
`TestHandleImportDependencyInvestigationFailsClosedWhenModuleMembershipOverflows`.
Reproduce:

```bash
cd go && go test ./internal/query -run TestImportDependency -count=1
```

**Query-plan bound coverage across the 244 valid request-shape combinations** —
`docs/internal/evidence/5561-import-investigation-bounds.md` records the #5561
fix for `POST /api/v0/code/imports/investigate` timeouts, replacing a single
query-plan registration with per-shape coverage (21-repository control
returning in <=5.7ms after the fix).

## Notes

No private data: this artifact cites only committed tests and a committed
evidence note, no deployment-specific values.

Related: #5552 (burn-down), #5407 (artifact-existence gate).
