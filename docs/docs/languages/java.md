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

## Dead-code Support

Java dead-code support is `derived`. Eshu suppresses parser-proven runtime
roots and uses reducer-produced `REFERENCES` edges when source or metadata
proves reachability, but it does not claim whole-program exactness for broad
reflection or dependency injection.

Supported roots and reachability evidence:

- JVM entrypoints: `main`, constructors, `@Override`, and serialization /
  Externalizable hook signatures.
- Build and framework callbacks: Ant `Task` setters, Gradle plugin `apply`,
  Gradle task actions/properties/setters, Gradle task-interface methods, and
  public Gradle DSL methods.
- Spring/JUnit/Jenkins/Stapler roots: Spring components, configuration
  properties, request mappings, beans, event listeners, scheduled methods,
  lifecycle callbacks, JUnit test/lifecycle methods, Jenkins extensions,
  symbols, initializers, data-bound setters, and Stapler web methods.
- Java reference evidence: same-class method references, bounded literal
  reflection (`Class.forName`, `ClassLoader.loadClass`,
  `SomeType.class.getMethod`, `SomeType.class.getDeclaredMethod`),
  ServiceLoader providers under META-INF/services, Spring Boot
  AutoConfiguration.imports, and legacy spring.factories.
- Overload-sensitive calls: parameter counts, argument counts, parameter types,
  class-literal argument types, typed lambda receivers, and same-class helper
  return types.
- Receiver evidence: local variables, parameters, fields, enhanced-for loop
  variables, explicit outer-this field receivers, records, and nested-class
  enclosing-class contexts.

Checked fixtures live in `tests/fixtures/ecosystems/java_comprehensive/`,
including `deadcode/RuntimeEntrypoints.java`, META-INF/services,
AutoConfiguration.imports, and spring.factories. Focused regression tests live
in `go/internal/parser/java_dead_code_fixture_test.go`,
`go/internal/parser/java_dead_code_serialization_roots_test.go`,
`go/internal/parser/java_reflection_test.go`,
`go/internal/parser/java_metadata_test.go`,
`go/internal/reducer/code_call_materialization_java_reflection_test.go`, and
`go/internal/reducer/code_call_materialization_java_metadata_test.go`.

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
| Dead-code root hints | `dead-code-root-hints` | supported | `functions.dead_code_root_kinds`, `classes.dead_code_root_kinds`, `function_calls.call_kind` | `name, line_number, dead_code_root_kinds, call_kind` | `graph:metadata + relationship:REFERENCES + code/dead-code policy` | `go/internal/parser/java_dead_code_roots_test.go::TestDefaultEngineParsePathJavaEmitsDeadCodeRootKinds`, `go/internal/parser/java_dead_code_framework_roots_test.go::TestDefaultEngineParsePathJavaMarksSpringFrameworkRoots`, `go/internal/parser/java_dead_code_framework_roots_test.go::TestDefaultEngineParsePathJavaMarksJenkinsFrameworkRoots`, `go/internal/parser/java_dead_code_serialization_roots_test.go::TestDefaultEngineParsePathJavaMarksSerializationRuntimeHooks`, `go/internal/parser/java_reflection_test.go::TestDefaultEngineParsePathJavaEmitsLiteralReflectionReferences`, `go/internal/parser/java_metadata_test.go::TestDefaultEngineParsePathJavaMetadataEmitsStaticClassReferences`, `go/internal/parser/java_dead_code_fixture_test.go::TestDefaultEngineParsePathJavaComprehensiveDeadCodeFixture`, `go/internal/query/code_dead_code_java_roots_test.go::TestHandleDeadCodeExcludesJavaRootKindsFromMetadata`, `go/internal/reducer/code_call_materialization_java_reflection_test.go::TestExtractCodeCallRowsResolvesJavaLiteralReflectionReferences`, `go/internal/reducer/code_call_materialization_java_metadata_test.go::TestExtractCodeCallRowsResolvesJavaMetadataClassReferencesFromFileRoot` | Local dogfood validation | Java runtime and framework roots are emitted as parser metadata or reducer reference edges and excluded from cleanup candidates by query policy. Java remains `derived` rather than `exact` while broad dynamic dispatch, broad reflection, and dependency injection are intentionally bounded. |

## Known Limitations
- Generic type bounds and wildcards are not captured as structured data beyond
  the receiver type leaf needed for local method-call resolution.
- Anonymous inner classes are not separately tracked.
- Lambda expressions are not individually modeled as functions.
- Dead-code does not infer arbitrary string-built reflection, container-driven
  dependency injection, runtime classpath scanning, or framework behavior that
  is not represented in source or checked metadata files.
