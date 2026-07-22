# prod-documentation-facts — production validation

Capability: `documentation_facts.list` (tool `list_documentation_facts`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: bounded_documentation_scope`, `p95_latency_ms: 1500`,
`max_truth_level: exact`.

## Claim validated

Bounded collected documentation facts by scope or document anchor, including
diagram facts and semantic-observation fact-kind discovery, with explicit
scoped-empty-page explanations rather than an ambiguous empty result.

## Committed reproducible evidence

**Handler contract, fact-kind discovery, and scoped-empty explanation** —
`go/internal/query/documentation_facts_test.go`:
`TestDocumentationHandlerListsCollectedFacts`,
`TestDocumentationHandlerListsDiagramFacts`,
`TestDocumentationHandlerRequiresFactScopeOrAnchor`, and
`TestDocumentationHandlerFactsResponseExplainsScopedEmptyPage`. Reproduce:

```bash
cd go && go test ./internal/query -run TestDocumentationHandler.*Facts -count=1
```

**Content-store filtering and search** —
`go/internal/query/documentation_facts_test.go`:
`TestContentReaderDocumentationFactsFiltersAndPaginates` and
`TestContentReaderDocumentationFactsSearchesLinkTargetURI`.

## Notes

No private data: cited tests use synthetic documentation fact fixtures; no
production credentials or deployment-specific values appear in this
artifact.

Related: #5407 (artifact-existence gate), #5552 (burn-down).
