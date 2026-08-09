# prod-documentation-facts — production validation

Validation-Slug: prod-documentation-facts
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: documentation_facts.list passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.
B12-Assertion: documentation_facts.list -> mcp:list_documentation_facts

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


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
