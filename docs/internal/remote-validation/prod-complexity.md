# prod-complexity — production validation

Validation-Slug: prod-complexity
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: code_quality.complexity passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


Capability: `code_quality.complexity` (tools `calculate_cyclomatic_complexity`,
`find_most_complex_functions`).
Production profile: `required_runtime: deployed_services`, `max_scope_size: multi_repo_platform`,
`p95_latency_ms: 2000`, `max_truth_level: derived`.

## Claim validated

Parser-derived cyclomatic complexity metric, servable by explicit function-name selector or as a
ranked "most complex functions" list, with ambiguity handling and truncation envelopes.

## Committed reproducible evidence

**Truncation envelope and ambiguous-name rejection** — `go/internal/query/code_complexity_contract_test.go`:
`TestHandleComplexityListReturnsTruncationInEnvelope` and
`TestHandleComplexityRejectsAmbiguousFunctionNameInEnvelope`. Reproduce:

```bash
cd go && go test ./internal/query -run TestHandleComplexity -count=1
```

**Selector-based and list-based complexity queries** — `go/internal/query/code_cypher_test.go`:
`TestHandleComplexityAcceptsFunctionNameSelector` and
`TestHandleComplexityListsMostComplexFunctionsWhenSelectorOmitted`. Reproduce:

```bash
cd go && go test ./internal/query -run "TestHandleComplexityAcceptsFunctionNameSelector|TestHandleComplexityListsMostComplexFunctionsWhenSelectorOmitted" -count=1
```

**Graph-metadata preservation across languages and backends** — `go/internal/query/code_call_graph_contract_test.go`:
`TestHandleComplexityPreservesPythonGraphMetadataWithoutContent`,
`TestHandleComplexityBuildsNonConflictingCypher`,
`TestHandleComplexityFallsBackToNameLookupWithinRepo`, and
`TestHandleComplexityPreservesTypeScriptGraphMetadataWithoutContent`; JavaScript semantics in
`go/internal/query/code_call_graph_javascript_semantics_test.go`:
`TestHandleComplexityReturnsGraphBackedJavaScriptSemantics`. Reproduce:

```bash
cd go && go test ./internal/query -run TestHandleComplexity -count=1
```

**Contract declaration** — `go/internal/query/openapi_complexity_test.go`:
`TestOpenAPISpecIncludesComplexityAmbiguityContract`. Reproduce:

```bash
cd go && go test ./internal/query -run TestOpenAPISpecIncludesComplexityAmbiguityContract -count=1
```

## Notes

No private data: fixtures use synthetic function names, repositories, and complexity scores only.

Related: #5552 (burn-down).
