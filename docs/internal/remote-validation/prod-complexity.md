# prod-complexity — production validation

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
