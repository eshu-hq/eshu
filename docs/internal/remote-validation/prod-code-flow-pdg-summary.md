# prod-code-flow-pdg-summary — production validation

Validation-Slug: prod-code-flow-pdg-summary
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: code_flow.pdg_summary passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


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
