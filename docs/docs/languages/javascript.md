# JavaScript Parser

This page tracks the checked-in Go parser contract in the current repository state.
Canonical implementation: `go/internal/parser/registry.go` plus the entrypoint and tests listed below.

## Parser Contract
- Language: `javascript`
- Family: `language`
- Parser: `DefaultEngine (javascript)`
- Entrypoint: `go/internal/parser/javascript_language.go`
- Fixture repo: `tests/fixtures/ecosystems/javascript_comprehensive/`
- Unit test suite: `go/internal/parser/engine_javascript_semantics_test.go`
- Integration validation: compose-backed fixture verification (see `../reference/local-testing.md`)

## Capability Checklist
| Capability | ID | Status | Extracted Bucket/Key | Required Fields | Graph Surface | Unit Coverage | Integration Coverage | Rationale |
|-----------|----|--------|------------------------|-----------------|---------------|---------------|----------------------|-----------|
| Function declarations | `function-declarations` | supported | `functions` | `name, line_number` | `node:Function` | `go/internal/parser/engine_test.go::TestDefaultEngineParsePathJavaScript` | Compose-backed fixture verification | - |
| Function expressions | `function-expressions` | supported | `functions` | `name, line_number` | `node:Function` | `go/internal/parser/engine_test.go::TestDefaultEngineParsePathJavaScript` | Compose-backed fixture verification | - |
| Arrow functions (named) | `arrow-functions-named` | supported | `functions` | `name, line_number` | `node:Function` | `go/internal/parser/engine_test.go::TestDefaultEngineParsePathJavaScript` | Compose-backed fixture verification | - |
| Method definitions | `method-definitions` | supported | `functions` | `name, line_number` | `node:Function` | `go/internal/parser/engine_test.go::TestDefaultEngineParsePathJavaScript` | Compose-backed fixture verification | - |
| Classes | `classes` | supported | `classes` | `name, line_number` | `node:Class` | `go/internal/parser/engine_test.go::TestDefaultEngineParsePathJavaScript` | Compose-backed fixture verification | - |
| Imports (`import`/`require`) | `imports-import-require` | supported | `imports` | `name, line_number` | `relationship:IMPORTS` | `go/internal/parser/engine_test.go::TestDefaultEngineParsePathJavaScript` | Compose-backed fixture verification | - |
| Function calls | `function-calls` | supported | `function_calls` | `name, line_number` | `relationship:CALLS` | `go/internal/parser/engine_test.go::TestDefaultEngineParsePathJavaScript` | Compose-backed fixture verification | - |
| Member call expressions | `member-call-expressions` | supported | `function_calls` | `name, line_number` | `relationship:CALLS` | `go/internal/parser/engine_test.go::TestDefaultEngineParsePathJavaScript` | Compose-backed fixture verification | - |
| Variables | `variables` | supported | `variables` | `name, line_number` | `node:Variable` | `go/internal/parser/engine_test.go::TestDefaultEngineParsePathJavaScript` | Compose-backed fixture verification | - |
| Generator functions | `generator-functions` | supported | `functions` | `name, line_number` | `node:Function` | `go/internal/parser/engine_javascript_semantics_test.go::TestDefaultEngineParsePathJavaScriptGeneratorFunctions` | Compose-backed fixture verification | - |
| JSDoc comments | `jsdoc-comments` | supported | `functions` | `name, line_number, docstring` | `graph-backed language-query metadata + graph-backed code/search metadata + graph-backed entities/resolve + graph-backed entity-context/story/call-chain surfaces` | `go/internal/parser/engine_javascript_semantics_test.go::TestDefaultEngineParsePathJavaScriptDocstringsAndMethodKinds`, `go/internal/query/javascript_semantics_test.go::TestHandleLanguageQueryJavaScriptMethodUsesGraphMetadataWithoutContent`, `go/internal/query/language_query_graph_first_test.go::TestHandleLanguageQuery_JavaScriptMethodPrefersGraphPathAndUsesGraphMetadataWithoutContent`, `go/internal/query/code_search_metadata_test.go::TestHandleSearchReturnsGraphBackedJavaScriptMetadataWithoutContent`, `go/internal/query/code_dead_code_javascript_semantics_test.go::TestHandleDeadCodeReturnsGraphBackedJavaScriptSemantics`, `go/internal/query/entity_content_fallback_test.go::TestResolveEntityReturnsGraphBackedJavaScriptFunctionWithJavaScriptSemantics`, `go/internal/query/language_query_metadata_test.go::TestEnrichLanguageResultsWithContentMetadataJavaScriptMethod`, `go/internal/query/entity_metadata_test.go::TestEnrichEntityResultsWithContentMetadata`, `go/internal/query/entity_summary_test.go::TestBuildEntitySemanticSummaryJavaScriptFunction`, `go/internal/query/entity_story_test.go::TestAttachSemanticSummaryAddsStoryForSemanticEntities`, `go/internal/query/entity_story_test.go::TestGetEntityContextUsesGraphJavaScriptMetadataWithoutContent`, `go/internal/query/repository_story_semantics_test.go::TestBuildRepositorySemanticOverviewCountsJavaScriptMethodSignals` | Compose-backed fixture verification | The Go parser emits docstring metadata, the graph-backed `language-query` path now returns that metadata directly from graph rows, `code/search` now preserves graph-provided JavaScript metadata when the graph already has it, `entities/resolve` now also preserves graph-backed JavaScript semantics when graph metadata is already present, and the shared graph-first handler now has explicit proof that `entities/{entity_id}/context` can also serve JavaScript function metadata without content fallback. The shared attachment path now also adds a dedicated `javascript_semantics` bundle to graph-backed query, search, resolve, context, story, and dead-code responses when docstring or method-kind metadata is present. The content pipeline still preserves the metadata for `code/relationships`, `code/complexity`, and the remaining non-JavaScript entity-context/story enrichment paths, and the shared query surfaces emit both a first-class semantic summary and a structured `semantic_profile`. Entity-context now also emits a first-class `story`, and repository stories carry a semantic overview derived from those same entities. |
| Method kind (get/set/async) | `method-kind-get-set-async` | supported | `functions` | `name, line_number, type` | `graph-backed language-query metadata + graph-backed code/search metadata + graph-backed entities/resolve + graph-backed entity-context/story/call-chain surfaces` | `go/internal/parser/engine_javascript_semantics_test.go::TestDefaultEngineParsePathJavaScriptDocstringsAndMethodKinds`, `go/internal/query/javascript_semantics_test.go::TestHandleLanguageQueryJavaScriptMethodUsesGraphMetadataWithoutContent`, `go/internal/query/language_query_graph_first_test.go::TestHandleLanguageQuery_JavaScriptMethodPrefersGraphPathAndUsesGraphMetadataWithoutContent`, `go/internal/query/code_search_metadata_test.go::TestHandleSearchReturnsGraphBackedJavaScriptMetadataWithoutContent`, `go/internal/query/code_dead_code_javascript_semantics_test.go::TestHandleDeadCodeReturnsGraphBackedJavaScriptSemantics`, `go/internal/query/entity_content_fallback_test.go::TestResolveEntityReturnsGraphBackedJavaScriptFunctionWithJavaScriptSemantics`, `go/internal/query/language_query_metadata_test.go::TestEnrichLanguageResultsWithContentMetadataJavaScriptMethod`, `go/internal/query/entity_metadata_test.go::TestEnrichEntityResultsWithContentMetadata`, `go/internal/query/entity_summary_test.go::TestBuildEntitySemanticSummaryJavaScriptFunction`, `go/internal/query/entity_story_test.go::TestAttachSemanticSummaryAddsStoryForSemanticEntities`, `go/internal/query/entity_story_test.go::TestGetEntityContextUsesGraphJavaScriptMetadataWithoutContent`, `go/internal/query/repository_story_semantics_test.go::TestBuildRepositorySemanticOverviewCountsJavaScriptMethodSignals` | Compose-backed fixture verification | Getter, setter, and async method kinds are captured and preserved in entity metadata, the graph-backed `language-query` path now returns that metadata directly from graph rows, `code/search` now preserves graph-provided JavaScript metadata when the graph already has it, `entities/resolve` now also preserves graph-backed JavaScript semantics when graph metadata is already present, and the shared graph-first handler now has explicit proof that `entities/{entity_id}/context` can also serve JavaScript function metadata without content fallback. The shared attachment path now also adds a dedicated `javascript_semantics` bundle to graph-backed query, search, resolve, context, story, and dead-code responses when docstring or method-kind metadata is present. The content pipeline still preserves it for the broader query surfaces, and the shared query surfaces emit both a first-class semantic summary and a structured `semantic_profile`. JavaScript method-kind rows now also get a dedicated `javascript_method` surface kind in the shared semantic profile. |

## Support Maturity
- Grammar routing: `supported`
- Normalization: `supported`
- Framework pack status: `supported`
- Framework packs: `react-base`, `nextjs-app-router-base`, `express-base`, `hapi-base`, `aws-sdk-base`, `gcp-sdk-base`
- Query surfacing: `supported`
- Real-repo validation: `supported`
- End-to-end indexing: `supported`
- Notes:
- Real-repo validation covers React, Next.js, Express, Hapi, and bounded AWS
  and GCP SDK evidence through the current parser and indexing path.
- JSDoc and method-kind semantics already survive parse and content
  materialization, the graph-backed `language-query` path now returns
  those fields directly from graph rows, and `code/search` now preserves
  graph-provided JavaScript metadata when it is already present on the
  graph row. `entities/{entity_id}/context` now also promotes graph-provided
  JavaScript function metadata into `semantic_summary`, `semantic_profile`,
  `javascript_semantics`, and `story` when the graph row already has it.
  `dead-code`, `code/relationships`, `code/complexity`, `code/call-chain`,
  and `entities/resolve` now also carry the same graph-backed JavaScript
  semantic bundle when the graph row already has it, while the remaining
  non-JavaScript entity-context/story paths still surface the matching
  metadata through content enrichment when the graph does not already have
  it. `language-query`, `code/search`, `entities/resolve`, `code/call-chain`,
  `dead-code`, `code/relationships`, and `code/complexity` now emit semantic
  summaries and a structured `semantic_profile` for JavaScript functions when
  docstring or method-kind metadata is present, JavaScript method-kind rows
  get a dedicated `javascript_method` surface kind, generator functions
  now surface `semantic_kind=generator`, entity-context still emits a
  first-class `story`, and repository stories still expose a semantic overview
  derived from those entities. The documented public JavaScript query
  surfaces are now graph-first; runtime-dependent computed expressions and
  dynamic `require()` targets remain bounded parser limitations, not parity
  blockers.


## Known Limitations
- Runtime-dependent computed expressions and dynamic `require()` targets remain bounded parser limitations in the current platform state.
