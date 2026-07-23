# prod-code-flow-taint-path — production validation

Capability: `code_flow.taint_path` (tools `dispatch_taint_path`,
`POST /api/v0/code/flow/taint-path`).
Production profile: `required_runtime: deployed_services`, `max_scope_size: multi_repo_platform`,
`p95_latency_ms: 1500`, `max_truth_level: derived`.

## Claim validated

Bounded active-generation read returning derived taint/interprocedural evidence
(`derived_reducer_evidence`); does not claim unsupported languages or whole-program taint.

## Committed reproducible evidence

**Derived taint evidence surfacing** — `go/internal/query/code_flow_test.go`:
`TestCodeFlowTaintPathSurfacesDerivedEvidence` (asserts the evidence handle shape, e.g.
`fact://code_taint_evidence/fact-1`). Reproduce:

```bash
cd go && go test ./internal/query -run TestCodeFlowTaintPathSurfacesDerivedEvidence -count=1
```

**Repository scope enforcement across the `/taint-path` route** — same file:
`TestCodeFlowScopedRepositoryFilterIsAppliedBeforeStoreRead` (table-driven across all four
`code_flow.*` routes including `taint-path`). Reproduce:

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

No private data: fixtures use synthetic repo/function identifiers and fact handles only.

Related: #5552 (burn-down).
