# prod-import-dependencies — production validation

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
