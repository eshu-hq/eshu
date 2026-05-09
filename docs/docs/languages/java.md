# Java Parser

This page tracks the checked-in Go parser contract in the current repository state.
Canonical implementation: `go/internal/parser/registry.go` plus the entrypoint and tests listed below.

## Parser Contract
- Language: `java`
- Family: `language`
- Parser: `DefaultEngine (java)`
- Entrypoint: `go/internal/parser/java_language.go`
- Fixture repo: `tests/fixtures/ecosystems/java_comprehensive/`
- Unit test suite: `go/internal/parser/engine_managed_oo_test.go`
- Integration validation: compose-backed fixture verification (see `../reference/local-testing.md`)

## Capability Checklist
| Capability | ID | Status | Extracted Bucket/Key | Required Fields | Graph Surface | Unit Coverage | Integration Coverage | Rationale |
|-----------|----|--------|------------------------|-----------------|---------------|---------------|----------------------|-----------|
| Methods | `methods` | supported | `functions` | `name, line_number, parameter_count` | `node:Function` | `go/internal/parser/engine_managed_oo_test.go::TestDefaultEngineParsePathJava`, `go/internal/parser/java_dead_code_roots_test.go::TestDefaultEngineParsePathJavaInfersLocalReceiverTypes` | Compose-backed fixture verification | Method arity is emitted so reducer call materialization can distinguish overloads. |
| Constructors | `constructors` | supported | `functions` | `name, line_number, parameter_count` | `node:Function` | `go/internal/parser/engine_managed_oo_test.go::TestDefaultEngineParsePathJava` | Compose-backed fixture verification | Constructor arity is emitted through the same function metadata path. |
| Classes | `classes` | supported | `classes` | `name, line_number` | `node:Class` | `go/internal/parser/engine_managed_oo_test.go::TestDefaultEngineParsePathJava` | Compose-backed fixture verification | - |
| Interfaces | `interfaces` | supported | `interfaces` | `name, line_number` | `node:Interface` | `go/internal/parser/engine_managed_oo_test.go::TestDefaultEngineParsePathJava` | Compose-backed fixture verification | - |
| Enums | `enums` | supported | `classes` | `name, line_number` | `node:Class` | `go/internal/parser/engine_managed_oo_test.go::TestDefaultEngineParsePathJava` | Compose-backed fixture verification | - |
| Annotation types | `annotation-types` | supported | `annotations` | `name, line_number` | `node:Annotation` | `go/internal/parser/engine_managed_oo_test.go::TestDefaultEngineParsePathJavaAnnotationMetadata` | Compose-backed fixture verification | - |
| Imports | `imports` | supported | `imports` | `name, line_number` | `relationship:IMPORTS` | `go/internal/parser/engine_managed_oo_test.go::TestDefaultEngineParsePathJava` | Compose-backed fixture verification | - |
| Method invocations | `method-invocations` | supported | `function_calls` | `name, line_number, argument_count` | `relationship:CALLS` | `go/internal/parser/engine_managed_oo_test.go::TestDefaultEngineParsePathJava`, `go/internal/parser/java_dead_code_roots_test.go::TestDefaultEngineParsePathJavaInfersLocalReceiverTypes` | Compose-backed fixture verification | Call arity is emitted so overload resolution can choose the matching method. |
| Object creation | `object-creation` | supported | `function_calls` | `name, line_number, argument_count` | `relationship:CALLS` | `go/internal/parser/engine_managed_oo_test.go::TestDefaultEngineParsePathJava` | Compose-backed fixture verification | Constructor-call arity follows the same call metadata contract. |
| Local receiver type inference | `receiver-type-inference` | supported | `function_calls.inferred_obj_type` | `name, full_name, inferred_obj_type, argument_count, line_number` | `relationship:CALLS` | `go/internal/parser/java_dead_code_roots_test.go::TestDefaultEngineParsePathJavaInfersLocalReceiverTypes`, `go/internal/reducer/code_call_materialization_java_test.go::TestExtractCodeCallRowsResolvesJavaReceiverCallsUsingInferredType` | Local dogfood validation | Receiver-qualified calls are typed when local syntax proves the receiver through a parameter, variable, field, or inline constructor expression. Reducer matching combines that type with arity metadata to avoid making all overloads live. |
| Local variables | `local-variables` | supported | `variables` | `name, line_number` | `node:Variable` | `go/internal/parser/engine_managed_oo_test.go::TestDefaultEngineParsePathJava` | Compose-backed fixture verification | - |
| Field declarations | `field-declarations` | supported | `variables` | `name, line_number` | `node:Variable` | `go/internal/parser/engine_managed_oo_test.go::TestDefaultEngineParsePathJava` | Compose-backed fixture verification | - |
| Annotations (applied) | `annotations-applied` | supported | `annotations` | `name, line_number, kind, target_kind` | `node:Annotation + graph-first code/language-query + entity-resolve/context/story` | `go/internal/parser/engine_managed_oo_test.go::TestDefaultEngineParsePathJavaAnnotationUsageKinds`, `go/internal/projector/runtime_test.go::TestRuntimeProjectEnqueuesSemanticEntityMaterializationForAnnotationAndTypedef`, `go/internal/reducer/semantic_entity_materialization_test.go::TestSemanticEntityMaterializationHandlerWritesAndRetracts`, `go/internal/storage/cypher/semantic_entity_test.go::TestSemanticEntityWriterWritesAnnotationAndTypedefNodes`, `go/internal/query/language_query_metadata_test.go::TestHandleLanguageQuery_AnnotationPrefersGraphPathAndEnrichesMetadata`, `go/internal/query/language_query_metadata_test.go::TestHandleLanguageQuery_AnnotationUsesGraphMetadataWithoutContent`, `go/internal/query/language_query_metadata_test.go::TestHandleLanguageQuery_AnnotationFallsBackToContentWhenGraphMissing`, `go/internal/query/entity_annotation_fallback_test.go::TestResolveEntityFallsBackToJavaAnnotationContentEntity`, `go/internal/query/entity_annotation_fallback_test.go::TestGetEntityContextFallsBackToJavaAnnotationContentEntity`, `go/internal/query/entity_story_test.go::TestAttachSemanticSummaryAddsStoryForSemanticEntities` | Compose-backed fixture verification | Applied annotations now persist as first-class `Annotation` graph nodes through the Go projector/reducer/Cypher graph path, remain graph-first on `code/language-query`, fall back to content when the graph is empty, and still surface humanized semantic summaries plus an `applied_annotation` semantic profile on resolve/context/story surfaces. |
| Dead-code root hints | `dead-code-root-hints` | supported | `functions.dead_code_root_kinds` | `name, line_number, dead_code_root_kinds` | `graph:metadata + code/dead-code policy` | `go/internal/parser/java_dead_code_roots_test.go::TestDefaultEngineParsePathJavaEmitsDeadCodeRootKinds`, `go/internal/query/code_dead_code_java_roots_test.go::TestHandleDeadCodeExcludesJavaRootKindsFromMetadata` | Local dogfood validation | Java `main` methods, constructors, and `@Override` methods are emitted as parser metadata and excluded from cleanup candidates by query policy. Java remains `derived` rather than `exact` while framework annotations, reflection, dependency injection, and service loader roots are still being modeled. |

## Known Limitations
- Generic type bounds and wildcards are not captured as structured data beyond
  the receiver type leaf needed for local method-call resolution.
- Anonymous inner classes not separately tracked
- Lambda expressions not individually modeled as functions
- Dead-code root modeling does not yet cover Jenkins extension annotations,
  Stapler web methods, service loader metadata, dependency injection, or
  reflection-heavy plugin registration.
