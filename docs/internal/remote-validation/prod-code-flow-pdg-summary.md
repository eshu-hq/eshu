# prod-code-flow-pdg-summary — production validation

Capability: `code_flow.pdg_summary` (tools `dispatch_pdg_summary`,
`POST /api/v0/code/flow/pdg-summary`).
Production profile: `required_runtime: deployed_services`, `max_scope_size: multi_repo_platform`,
`p95_latency_ms: 1500`, `max_truth_level: derived`.

## Claim validated

Bounded partial `derived_summary` combining available parser def-use and control-dependence
rows; not a whole-program PDG. Ambiguous symbols and stale generations stay explicit rather than
being silently resolved.

## Committed reproducible evidence

**Ambiguity and staleness stay explicit** — `go/internal/query/code_flow_test.go`:
`TestCodeFlowPDGSummaryAmbiguousSymbolAndStaleGenerationStayExplicit` (asserts
`coverage.state=partial`, `truth.freshness.state=stale`, and an explicit ambiguity candidate
list rather than a silently-picked symbol). Reproduce:

```bash
cd go && go test ./internal/query -run TestCodeFlowPDGSummaryAmbiguousSymbolAndStaleGenerationStayExplicit -count=1
```

**Repository scope enforcement across all four `code_flow.*` routes (including PDG summary)** —
same file: `TestCodeFlowScopedRepositoryFilterIsAppliedBeforeStoreRead`. Reproduce:

```bash
cd go && go test ./internal/query -run TestCodeFlowScopedRepositoryFilterIsAppliedBeforeStoreRead -count=1
```

**Underlying active-generation read model correctness** — `go/internal/query/code_flow_postgres_test.go`:
`TestCodeFlowSQLReadsLatestFactPerStableKeyThroughActiveGeneration`. Reproduce:

```bash
cd go && go test ./internal/query -run TestCodeFlowSQLReadsLatestFactPerStableKeyThroughActiveGeneration -count=1
```

**Contract declaration** — `go/internal/query/openapi_code_flow_test.go`:
`TestOpenAPIDocumentsCodeFlowRoutes`. Reproduce:

```bash
cd go && go test ./internal/query -run TestOpenAPIDocumentsCodeFlowRoutes -count=1
```

## Notes

No private data: fixtures use synthetic repo/function identifiers and def-use/control-dependence
shapes only.

Related: #5552 (burn-down).
