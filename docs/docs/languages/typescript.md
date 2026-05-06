# TypeScript Parser

This page tracks the checked-in Go parser contract in the current repository state.
Canonical implementation: `go/internal/parser/registry.go` plus the entrypoint and tests listed below.

## Parser Contract
- Language: `typescript`
- Family: `language`
- Parser: `DefaultEngine (typescript)`
- Entrypoint: `go/internal/parser/javascript_language.go`
- Fixture repo: `tests/fixtures/ecosystems/typescript_comprehensive/`
- Unit test suite: `go/internal/parser/engine_javascript_semantics_test.go`
- Integration validation: compose-backed fixture verification (see `../reference/local-testing.md`)

## Capability Checklist
| Capability | ID | Status | Extracted Bucket/Key | Required Fields | Graph Surface | Unit Coverage | Integration Coverage | Rationale |
|-----------|----|--------|------------------------|-----------------|---------------|---------------|----------------------|-----------|
| Functions | `functions` | supported | `functions` | `name, line_number` | `node:Function` | `go/internal/parser/engine_test.go::TestDefaultEngineParsePathTypeScript` | Compose-backed fixture verification | - |
| Classes | `classes` | supported | `classes` | `name, line_number` | `node:Class` | `go/internal/parser/engine_test.go::TestDefaultEngineParsePathTypeScript` | Compose-backed fixture verification | - |
| Interfaces | `interfaces` | supported | `interfaces` | `name, line_number` | `node:Interface` | `go/internal/parser/engine_test.go::TestDefaultEngineParsePathTypeScript` | Compose-backed fixture verification | - |
| Imports | `imports` | supported | `imports` | `name, line_number` | `relationship:IMPORTS` | `go/internal/parser/engine_test.go::TestDefaultEngineParsePathTypeScript` | Compose-backed fixture verification | - |
| Function calls | `function-calls` | supported | `function_calls` | `name, line_number` | `relationship:CALLS` | `go/internal/parser/engine_test.go::TestDefaultEngineParsePathTypeScript` | Compose-backed fixture verification | - |
| Variables | `variables` | supported | `variables` | `name, line_number` | `node:Variable` | `go/internal/parser/engine_test.go::TestDefaultEngineParsePathTypeScript` | Compose-backed fixture verification | - |
| Enums | `enums` | supported | `enums` | `name, line_number` | `node:Enum` | `go/internal/parser/engine_javascript_semantics_test.go::TestDefaultEngineParsePathTypeScriptSemanticsAndTypes` | Compose-backed fixture verification | - |
| Type aliases | `type-aliases` | supported | `type_aliases` | `name, line_number` | `node:TypeAlias + graph-first code/language-query + entity_context.story` | `go/internal/query/language_query_graph_first_test.go::TestHandleLanguageQuery_TypeAliasPrefersGraphPathAndUsesGraphMetadataWithoutContent`, `go/internal/projector/runtime_test.go::TestRuntimeProjectEnqueuesSemanticEntityMaterializationForAnnotationTypedefTypeAliasAndComponent`, `go/internal/reducer/semantic_entity_materialization_test.go::TestExtractSemanticEntityRowsFiltersAnnotationTypedefTypeAliasAndComponentFacts`, `go/internal/storage/cypher/semantic_entity_test.go::TestSemanticEntityWriterWritesAnnotationTypedefTypeAliasAndComponentNodes`, `go/internal/query/entity_content_fallback_test.go::TestResolveEntityFallsBackToContentEntities`, `go/internal/query/content_reader_test.go::TestCodeHandlerSearchEntityContentIncludesEntityNameMatches` | Compose-backed fixture verification | Type aliases now persist as first-class `TypeAlias` graph nodes through the Go projector/reducer/Cypher graph path, the normal `code/language-query` surface prefers graph rows before falling back to content, and normal entity resolve/context plus `code/search` still preserve the fallback path when the graph is empty. |
| Decorators | `decorators` | supported | `classes` | `name, line_number` | `content:Entity.metadata.decorators + code/language/entity_context.story + repository_story.semantic_overview` | `go/internal/parser/engine_javascript_semantics_test.go::TestDefaultEngineParsePathTypeScriptDecoratorAndGenericParity`, `go/internal/query/language_query_metadata_test.go::TestEnrichLanguageResultsWithContentMetadata`, `go/internal/query/code_search_metadata_test.go::TestEnrichGraphSearchResultsWithContentMetadata`, `go/internal/query/entity_metadata_test.go::TestEnrichEntityResultsWithContentMetadata`, `go/internal/query/entity_story_test.go::TestAttachSemanticSummaryAddsStoryForSemanticEntities`, `go/internal/query/repository_story_semantics_test.go::TestBuildRepositoryStoryResponseIncludesSemanticOverview` | Compose-backed fixture verification | Decorator metadata is emitted and preserved in content entities, and graph-backed `language-query`, `code/search`, `dead-code`, `code/relationships`, `code/complexity`, `entities/resolve`, and entity-context responses enrich matching rows with that metadata while also emitting a structured `semantic_profile`; entity-context emits a first-class `story`, and repository stories carry a semantic overview derived from those same entities. |
| Generics | `generics` | supported | `type_parameters` | `name, line_number, type_parameters` | `graph-backed canonical Function/Class/Interface/Enum metadata + code/language/entity_context.story + repository_story.semantic_overview` | `go/internal/parser/engine_javascript_semantics_test.go::TestDefaultEngineParsePathTypeScriptDecoratorAndGenericParity`, `go/internal/parser/engine_javascript_semantics_test.go::TestDefaultEngineParsePathTypeScriptCapturesNestedGenericTypeParameters`, `go/internal/query/language_query_metadata_test.go::TestEnrichLanguageResultsWithContentMetadata`, `go/internal/query/code_search_metadata_test.go::TestEnrichGraphSearchResultsWithContentMetadata`, `go/internal/query/entity_metadata_test.go::TestEnrichEntityResultsWithContentMetadata`, `go/internal/query/entity_story_test.go::TestAttachSemanticSummaryAddsStoryForSemanticEntities`, `go/internal/query/repository_story_semantics_test.go::TestBuildRepositoryStoryResponseIncludesSemanticOverview` | Compose-backed fixture verification | Type parameter metadata is preserved in content entities, canonical TypeScript `Function`, `Class`, and `Interface` rows persist `type_parameters` on the graph-backed path, and those values flow into graph-backed `language-query`, `code/search`, `dead-code`, `code/relationships`, `code/complexity`, `entities/resolve`, and entity-context responses for matching entities, including a structured `semantic_profile`; the parser keeps nested generic constraints and defaults intact enough to recover the declared type parameter names, entity-context emits a first-class `story`, and repository stories carry a semantic overview derived from those same entities. |
| Mapped types | `mapped-types` | supported | `type_aliases` | `name, line_number, type_alias_kind=mapped_type` | `node:TypeAlias + graph-first code/language-query + entity_context.story` | `go/internal/parser/engine_typescript_advanced_semantics_test.go::TestDefaultEngineParsePathTypeScriptCapturesAdvancedTypeSemantics`, `go/internal/query/typescript_graph_metadata_test.go::TestHandleLanguageQueryProjectsTypeScriptGraphMetadata`, `go/internal/query/typescript_graph_metadata_test.go::TestCodeSearchProjectsTypeScriptGraphMetadata`, `go/internal/query/typescript_graph_metadata_test.go::TestGetEntityContextProjectsTypeScriptGraphMetadata` | Compose-backed fixture verification | The Go parser now tags mapped type aliases directly, and the graph-backed query/context/search surfaces now project `type_alias_kind` and `type_parameters` from Neo4j rows when present. Mapped aliases are first-class TypeAlias graph entities in the current platform state. |
| Conditional types | `conditional-types` | supported | `type_aliases` | `name, line_number, type_alias_kind=conditional_type` | `node:TypeAlias + graph-first code/language-query + entity_context.story` | `go/internal/parser/engine_typescript_advanced_semantics_test.go::TestDefaultEngineParsePathTypeScriptCapturesAdvancedTypeSemantics`, `go/internal/query/typescript_graph_metadata_test.go::TestHandleLanguageQueryProjectsTypeScriptGraphMetadata`, `go/internal/query/typescript_graph_metadata_test.go::TestCodeSearchProjectsTypeScriptGraphMetadata`, `go/internal/query/typescript_graph_metadata_test.go::TestGetEntityContextProjectsTypeScriptGraphMetadata` | Compose-backed fixture verification | Conditional type aliases now survive on the Go parser/content/query path with dedicated semantic promotion, and the graph-backed query/context/search surfaces now project their `type_alias_kind` and `type_parameters` metadata directly from Neo4j rows. Conditional aliases are first-class TypeAlias graph entities in the current platform state. |
| Namespaces | `namespaces` | supported | `modules` | `name, line_number, module_kind=namespace` | `node:Module + graph-first code/language-query + entity_context.story` | `go/internal/parser/engine_typescript_advanced_semantics_test.go::TestDefaultEngineParsePathTypeScriptCapturesAdvancedTypeSemantics`, `go/internal/query/language_query_graph_first_test.go::TestHandleLanguageQuery_TypeScriptNamespaceUsesGraphMetadataWithoutContent`, `go/internal/query/entity_metadata_typescript_semantics_test.go::TestEnrichEntityResultsWithContentMetadataTypeScriptNamespaceModule`, `go/internal/reducer/semantic_entity_materialization_module_test.go::TestExtractSemanticEntityRowsIncludesTypeScriptModuleFacts`, `go/internal/projector/semantic_entity_intents_test.go::TestBuildSemanticEntityReducerIntentQueuesTypeScriptModuleSemanticEntities`, `go/internal/storage/cypher/semantic_entity_test.go::TestSemanticEntityWriterWritesTypeScriptModuleSemanticMetadata` | Compose-backed fixture verification | TypeScript namespace declarations now materialize as first-class `Module` graph nodes through the Go projector/reducer/Cypher graph path, and the shared graph-backed `language-query` path can summarize namespace rows directly when the graph already carries `module_kind`. |
| Declaration merging | `declaration-merging` | supported | `functions`, `classes`, `interfaces`, `modules`, `enums` | `name, declaration_merge_group, declaration_merge_count, declaration_merge_kinds` | `node:Module + graph-first code/language-query + entity_context.story` | `go/internal/parser/engine_typescript_advanced_semantics_test.go::TestDefaultEngineParsePathTypeScriptCapturesDeclarationMerging`, `go/internal/query/language_query_graph_first_test.go::TestHandleLanguageQuery_TypeScriptDeclarationMergeUsesGraphMetadataWithoutContent`, `go/internal/query/entity_metadata_typescript_semantics_test.go::TestEnrichEntityResultsWithContentMetadataTypeScriptDeclarationMerging`, `go/internal/reducer/semantic_entity_materialization_module_test.go::TestExtractSemanticEntityRowsIncludesTypeScriptModuleFacts`, `go/internal/projector/semantic_entity_intents_test.go::TestBuildSemanticEntityReducerIntentQueuesTypeScriptModuleSemanticEntities`, `go/internal/storage/cypher/semantic_entity_test.go::TestSemanticEntityWriterWritesTypeScriptModuleSemanticMetadata` | Compose-backed fixture verification | Same-file TypeScript declaration merging between supported declarations now survives the Go parser, is surfaced through semantic summaries, profiles, and stories on the normal content/query path, and is now also persisted as first-class `Module` graph nodes through the Go projector/reducer/Cypher graph path when matching graph rows already carry the merge metadata. |

## Support Maturity
- Grammar routing: `supported`
- Normalization: `supported`
- Framework pack status: `supported`
- Framework packs: `react-base`, `nextjs-app-router-base`, `express-base`, `hapi-base`, `aws-sdk-base`, `gcp-sdk-base`
- Query surfacing: `supported`
- Real-repo validation: `supported`
- End-to-end indexing: `supported`
- Notes:
  - Real-repo validation covers pure TypeScript repositories without requiring
    TSX-specific framework evidence.
  - TypeScript uses the same declarative Node HTTP and provider-pack framework
    families as JavaScript.
- Type aliases now persist as first-class `TypeAlias` graph nodes through
  the Go projector/reducer/Cypher graph path, and the graph-backed `code/language-query`,
  `code/search`, `entities/resolve`, and entity-context surfaces now project
  `type_alias_kind`, `type_parameters`, and related declaration-merge
  metadata directly from Neo4j rows. Mapped and conditional aliases are now
  first-class graph-backed entities in the current platform state, including wrapped
  conditional aliases that the parser normalizes.
- Graph-backed TypeScript query surfaces now also attach a dedicated
  `typescript_semantics` bundle when class-family rows already carry
  decorators, type parameters, or declaration-merge metadata. That bundle now
  shows up on the shared `code/search`, `code/dead-code`, `entities/resolve`,
  `code/relationships`, `code/complexity`, `code/language-query`, and
  entity-context/story paths.
- Canonical TypeScript `Class`, `Interface`, `Enum`, and decorated/generic
  `Function` rows now preserve `decorators`, `type_parameters`, and
  `declaration_merge_*` metadata through the Go projector and Neo4j writer
  when those values are present in the entity payloads.
- Graph-backed `language-query`, `code/search`, and entity-context results
  now also surface `decorators`, `type_parameters`, and
  `declaration_merge_*` directly from Neo4j rows when those semantic entities
  were persisted by the graph writer.
- The same graph-backed rows now also attach `typescript_semantics`, which
  keeps the TypeScript query surfaces aligned when the graph already has the
  canonical class-family metadata.


## Known Limitations
- No known graph-first gaps remain on the documented TypeScript surfaces.
