# prod-decorators — production validation

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
