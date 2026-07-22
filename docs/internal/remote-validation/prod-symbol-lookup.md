# prod-symbol-lookup — production validation

Capability: `code_search.symbol_lookup` (tool `find_symbol`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: multi_repo_platform`, `p95_latency_ms: 800`,
`max_truth_level: exact`.

## Claim validated

Bounded content-index symbol definition lookup.

## Committed reproducible evidence

**Bounded definition lookup, offset/backend validation** —
`go/internal/query/code_symbol_test.go`:
`TestCodeHandlerSymbolSearchReturnsBoundedContentDefinitions`,
`TestCodeHandlerFindSymbolRejectsHugeOffset`,
`TestCodeHandlerFindSymbolRejectsGraphOnlyOffset`, and
`TestCodeHandlerFindSymbolRejectsMissingBackends`. Reproduce:

```bash
cd go && go test ./internal/query -run 'TestCodeHandlerSymbolSearchReturnsBoundedContentDefinitions|TestCodeHandlerFindSymbol' -count=1
```

## Notes

No private data: cited tests exercise fixture symbol definitions only.

Related: #5552 (burn-down).
