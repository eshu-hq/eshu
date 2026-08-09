# prod-code-flow-cfg-summary — production validation

Validation-Slug: prod-code-flow-cfg-summary
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: code_flow.cfg_summary passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


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
