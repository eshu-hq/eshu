# prod-code-flow-cfg-summary — production validation

Capability: `code_flow.cfg_summary` (tools `dispatch_cfg_summary`,
`POST /api/v0/code/flow/cfg-summary`).
Production profile: `required_runtime: deployed_services`, `max_scope_size: multi_repo_platform`,
`p95_latency_ms: 1500`, `max_truth_level: exact`.

## Claim validated

Bounded exact-parser-fact read over parser-emitted `dataflow_functions` CFG blocks and edges; no
semantic provider key required.

## Committed reproducible evidence

**Exact parser fact surfacing and bounds** — `go/internal/query/code_flow_test.go`:
`TestCodeFlowCFGSummarySurfacesExactParserFactsAndBounds`. Reproduce:

```bash
cd go && go test ./internal/query -run TestCodeFlowCFGSummarySurfacesExactParserFactsAndBounds -count=1
```

**Repository scope enforcement across all four `code_flow.*` routes (including CFG summary)** —
same file: `TestCodeFlowScopedRepositoryFilterIsAppliedBeforeStoreRead`. Reproduce:

```bash
cd go && go test ./internal/query -run TestCodeFlowScopedRepositoryFilterIsAppliedBeforeStoreRead -count=1
```

**Underlying active-generation read model correctness** — `go/internal/query/code_flow_postgres_test.go`:
`TestCodeFlowSQLReadsLatestFactPerStableKeyThroughActiveGeneration` and
`TestCodeFlowSQLKeepsLiteralKindConjunctForPartialIndex`. Reproduce:

```bash
cd go && go test ./internal/query -run TestCodeFlowSQL -count=1
```

**Contract declaration** — `go/internal/query/openapi_code_flow_test.go`:
`TestOpenAPIDocumentsCodeFlowRoutes`. Reproduce:

```bash
cd go && go test ./internal/query -run TestOpenAPIDocumentsCodeFlowRoutes -count=1
```

## Notes

No private data: fixtures use synthetic repo/function identifiers and CFG block/edge shapes only.

Related: #5552 (burn-down).
