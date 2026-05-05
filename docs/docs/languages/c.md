# C Parser

This page tracks the checked-in Go parser contract in the current repository state.
Canonical implementation: `go/internal/parser/registry.go` plus the entrypoint and tests listed below.

## Parser Contract
- Language: `c`
- Family: `language`
- Parser: `DefaultEngine (c)`
- Entrypoint: `go/internal/parser/c_language.go`
- Fixture repo: `tests/fixtures/ecosystems/c_comprehensive/`
- Unit test suite: `go/internal/parser/engine_systems_test.go`
- Integration validation: compose-backed fixture verification (see `../reference/local-testing.md`)

## Capability Checklist
| Capability | ID | Status | Extracted Bucket/Key | Required Fields | Graph Surface | Unit Coverage | Integration Coverage | Rationale |
|-----------|----|--------|------------------------|-----------------|---------------|---------------|----------------------|-----------|
| Functions | `functions` | supported | `functions` | `name, line_number` | `node:Function` | `go/internal/parser/engine_systems_test.go::TestDefaultEngineParsePathC` | Compose-backed fixture verification | - |
| Pointer-returning functions | `pointer-returning-functions` | supported | `functions` | `name, line_number` | `node:Function` | `go/internal/parser/engine_systems_test.go::TestDefaultEngineParsePathC` | Compose-backed fixture verification | - |
| Structs | `structs` | supported | `classes` | `name, line_number` | `node:Class` | `go/internal/parser/engine_systems_test.go::TestDefaultEngineParsePathC` | Compose-backed fixture verification | - |
| Unions | `unions` | supported | `classes` | `name, line_number` | `node:Class` | `go/internal/parser/engine_systems_test.go::TestDefaultEngineParsePathCTypedefAliases` | Compose-backed fixture verification | - |
| Enums | `enums` | supported | `classes` | `name, line_number` | `node:Class` | `go/internal/parser/engine_systems_test.go::TestDefaultEngineParsePathC` | Compose-backed fixture verification | - |
| Typedefs | `typedefs` | supported | `typedefs` | `name, line_number` | `node:Typedef + graph-first code/language-query + entity-resolve/context + semantic_summary, with content fallback` | `go/internal/parser/engine_systems_test.go::TestDefaultEngineParsePathCTypedefAliasEmitsDedicatedEntities`, `go/internal/projector/runtime_test.go::TestRuntimeProjectEnqueuesSemanticEntityMaterializationForAnnotationAndTypedef`, `go/internal/reducer/semantic_entity_materialization_test.go::TestSemanticEntityMaterializationHandlerWritesAndRetracts`, `go/internal/storage/cypher/semantic_entity_test.go::TestSemanticEntityWriterWritesAnnotationAndTypedefNodes`, `go/internal/query/language_query_graph_first_test.go::TestHandleLanguageQuery_CTypedefPrefersGraphPathAndEnrichesMetadata`, `go/internal/query/language_query_graph_first_test.go::TestHandleLanguageQuery_CTypedefUsesGraphMetadataWithoutContent`, `go/internal/query/entity_summary_test.go::TestBuildEntitySemanticSummaryTypedef`, `go/internal/query/entity_content_c_fallback_test.go::TestResolveEntityFallsBackToContentTypedefEntity`, `go/internal/query/entity_content_c_fallback_test.go::TestGetEntityContextFallsBackToContentTypedefEntity` | Compose-backed fixture verification | The Go parser emits dedicated typedef entities, the projector/reducer/Cypher graph path now persists them as first-class `Typedef` graph nodes, the normal Go language-query path prefers graph rows before falling back to content, and entity-resolve/context surfaces return them with semantic summaries. |
| Includes | `includes` | supported | `imports` | `name, line_number` | `relationship:IMPORTS` | `go/internal/parser/engine_systems_test.go::TestDefaultEngineParsePathC` | Compose-backed fixture verification | - |
| Function calls | `function-calls` | supported | `function_calls` | `name, line_number` | `relationship:CALLS` | `go/internal/parser/engine_systems_test.go::TestDefaultEngineParsePathC` | Compose-backed fixture verification | - |
| Variables (initialized declarations) | `variables-initialized-declarations` | supported | `variables` | `name, line_number` | `node:Variable` | `go/internal/parser/engine_systems_test.go::TestDefaultEngineParsePathC` | Compose-backed fixture verification | - |
| Macros (`#define`) | `macros-define` | supported | `macros` | `name, line_number` | `node:Macro` | `go/internal/parser/engine_systems_test.go::TestDefaultEngineParsePathC` | Compose-backed fixture verification | - |

## Known Limitations
- Function pointer declarations are not modeled as callable entities
- Preprocessor macros with complex expansions are captured by name only
- Variadic functions do not expose their variadic argument types
