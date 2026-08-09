# prod-decorators — production validation

Validation-Slug: prod-decorators
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: symbol_graph.decorators passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.
B12-Assertion: symbol_graph.decorators -> mcp:execute_language_query

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


Capability: `symbol_graph.decorators` (tool `execute_language_query`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: multi_repo_platform`, `p95_latency_ms: 1200`,
`max_truth_level: exact`.

## Claim validated

Durable semantic decorator facts are returned through the language-query
metadata enrichment path, sourced from parser-emitted per-entity metadata
(e.g. Python `@route`/`@tracked`) rather than a best-effort content scan.

## Committed reproducible evidence

**Decorator metadata enrichment contract** —
`go/internal/query/language_query_metadata_test.go`:
`TestEnrichLanguageResultsWithContentMetadata` (asserts the `decorators`
field and generated `semantic_summary` from parser metadata) and
`TestEnrichLanguageResultsWithContentMetadataPreservesPythonGraphMetadata`.
Reproduce:

```bash
cd go && go test ./internal/query -run TestEnrichLanguageResultsWithContentMetadata -count=1
```

**Graph-backed decorator entity context** —
`go/internal/query/entity_story_test.go`:
`TestGetEntityContextUsesGraphPythonDecoratedClassWithoutContent` proves
decorator facts resolve from graph metadata even without a content-store
hydration path.

## Notes

No private data: cited tests use synthetic Python fixture source; no
production credentials or deployment-specific values appear in this
artifact.

Related: #5407 (artifact-existence gate), #5552 (burn-down).
