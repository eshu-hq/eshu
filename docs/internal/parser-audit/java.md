# Java Parser Audit

## Overview
The Java parser is the most mature and deeply-tested language adapter in Eshu.
It uses tree-sitter AST traversal for all source-code extraction (classes,
interfaces, annotations, enums, records, methods, constructors, fields, local
variables, imports, calls, method references, object creations) with a layered
call-inference index, dead-code root classification (15+ root kinds), static
reflection references, ServiceLoader/Spring metadata, and opt-in value-flow
analysis (taint, interproc, summaries, sources). The test suite is the largest
of any parser: 41 external `java_test` engine tests in `go/internal/parser/java`
(the 40 in the `java_*_test.go` family relocated from the parent by #6062, plus
the implemented-interfaces regression), 12 in-package tests, and the Java cases
of the parent's `engine_managed_oo_test.go`, covering every entity type, root
kind, inference path, metadata format, and value-flow bucket.

## Claimed Constructs
- **Classes, interfaces, annotations (declarations + applied), enums, records**
  from tree-sitter AST nodes (`parser.go:46-66`)
- **Methods and constructors** with decorators, parameter types, parameter
  counts, class context, dead-code roots (`parser.go:67-68`, `parser.go:135-172`)
- **Fields** as variables bucket (`parser.go:69-72`)
- **Local variables** (scope-gated) as variables bucket (`parser.go:73-79`)
- **Imports** with full_import_name, import_type (static/import), alias
  (`parser.go:80-81`, `parser.go:242-265`)
- **Method invocations** with full_name, inferred_obj_type, class_context,
  argument_types (`parser.go:82-83`, `parser.go:278-359`)
- **Method references** (`ClassName::method`) as calls with call_kind
  `java.method_reference` (`parser.go:290-303`)
- **Object creation expressions** (`new Foo()`) as calls with inferred_type
  (`parser.go:84-85`, `parser.go:304-309`)
- **Dead-code roots (methods)**: java.main_method, java.override_method,
  java.constructor, java.ant_task_setter, java.gradle_task_setter,
  java.gradle_task_interface_method, java.gradle_plugin_apply,
  java.gradle_task_action, java.gradle_task_property,
  java.gradle_dsl_public_method, java.method_reference_target,
  java.serialization_hook_method, java.externalizable_hook_method, plus
  Spring (@RequestMapping, @GetMapping, @PostMapping, @PutMapping,
  @DeleteMapping, @PatchMapping, @Bean, @EventListener, @Scheduled,
  @PreAuthorize, @PostAuthorize, @KafkaListener, @JmsListener,
  @RabbitListener, @MessageMapping, @Transactional, @Cacheable,
  @CacheEvict, @CachePut, @Async), JUnit (@Test, @BeforeEach, @AfterEach,
  @BeforeAll, @AfterAll, @ParameterizedTest, @RepeatedTest,
  @TestFactory), Jenkins (@Extension, @Initializer, @DataBoundConstructor,
  @DataBoundSetter) (`dead_code_roots.go:14-65`,
  `javaFrameworkMethodRootKinds`, `parser_metadata.go`)
- **Dead-code roots (types)**: java.spring_component_class,
  java.spring_configuration_properties_class, java.jenkins_extension_class,
  java.jenkins_symbol_class (`dead_code_roots.go:67-95`)
- **Implemented interfaces** on classes (`parser.go:127-131`)
- **Call inference**: receiver type, qualified type, argument types, class
  context chain, enclosing_class_contexts (`call_inference.go`,
  `call_context.go`, `type_inference_helpers.go`)
- **Reflection references** (literal only): Class.forName, loadClass,
  getMethod, getDeclaredMethod (`reflection.go:16-32`)
- **Java metadata**: ServiceLoader providers (`META-INF/services/*`),
  Spring auto-configuration (`spring.factories`,
  `AutoConfiguration.imports`) (`metadata.go`)
- **Value-flow (opt-in via Options.EmitDataflow)**: dataflow_functions,
  taint_findings, interproc_findings, dataflow_summaries, dataflow_sources
  with Spring request-param sources and JDBC/JPA sinks
  (`dataflow_emit.go`, `dataflow_lower.go`, `dataflow_taint.go`,
  `dataflow_summary.go`, `dataflow_bindings.go`)
- **Cyclomatic complexity** via shared McCabe walker with Java branch set
  (`complexity.go`)
- **PreScan** names from all declaration types (`parser.go:175-204`)

## Verified-by-Test Constructs
- **All entity types**: classes, interfaces, enums, annotations (declaration +
  applied), methods, constructors, fields, local variables, imports, calls:
  `engine_managed_oo_test.go:TestDefaultEngineParsePathJava` (line 11),
  `engine_managed_oo_test.go:TestDefaultEngineParsePathJavaAnnotationMetadata` (line 67),
  `engine_managed_oo_test.go:TestDefaultEngineParsePathJavaAnnotationUsageKinds` (line 88),
  `java/java_dead_code_roots_test.go:TestDefaultEngineParsePathJavaEmitsDeadCodeRootKinds` (line 14)
- **Records**:
  `java/java_dead_code_maturity_test.go:TestDefaultEngineParsePathJavaModelsRecordsAndThisFieldReceivers` (line 94)
- **Implemented interfaces**:
  `java/engine_java_implements_test.go:TestDefaultEngineParsePathJavaEmitsImplementedInterfaces` (line 13)
- **Imports with import_type, alias, full_import_name**:
  verified in `engine_managed_oo_test.go`
- **Method/constructor/class dead-code root kinds** (~15 root kinds):
  `java/java_dead_code_roots_test.go:TestDefaultEngineParsePathJavaEmitsDeadCodeRootKinds`,
  `java/java_dead_code_roots_test.go:TestDefaultEngineParsePathJavaMarksAntTaskSettersAsRoots` (line 57),
  `java/java_dead_code_roots_test.go:TestDefaultEngineParsePathJavaMarksGradleRoots` (line 103)
- **Spring framework roots**:
  `java/java_dead_code_framework_roots_test.go:TestDefaultEngineParsePathJavaMarksSpringFrameworkRoots` (line 14)
- **JUnit roots**:
  `java/java_dead_code_framework_roots_test.go:TestDefaultEngineParsePathJavaMarksJUnitRoots` (line 93)
- **Jenkins roots**:
  `java/java_dead_code_framework_roots_test.go:TestDefaultEngineParsePathJavaMarksJenkinsFrameworkRoots` (line 152)
- **Method reference targets**:
  `java/java_dead_code_roots_test.go:TestDefaultEngineParsePathJavaMarksDeclaredTypeMethodReferenceTargets` (line 294),
  `java/java_dead_code_roots_test.go:TestDefaultEngineParsePathJavaMarksInterfaceEnumAndRecordMethodReferenceTargets` (line 337),
  `java/java_dead_code_roots_test.go:TestDefaultEngineParsePathJavaDoesNotMarkDuplicateDeclaredTypeMethodReferenceTargets` (line 391),
  `java/java_dead_code_maturity_test.go:TestDefaultEngineParsePathJavaMarksMethodReferenceTargets` (line 55)
- **Method reference calls with metadata**:
  `java/java_dead_code_roots_test.go:TestDefaultEngineParsePathJavaEmitsMethodReferenceCalls` (line 208),
  `java/java_dead_code_roots_test.go:TestDefaultEngineParsePathJavaEmitsTypedMethodReferenceMetadata` (line 251)
- **Local receiver type inference**:
  `java/java_dead_code_roots_test.go:TestDefaultEngineParsePathJavaInfersLocalReceiverTypes` (line 477),
  `java/java_dead_code_roots_test.go:TestDefaultEngineParsePathJavaAddsClassContextToUnqualifiedMethodCalls` (line 522)
- **Enhanced for receiver inference**:
  `java/java_call_context_test.go:TestDefaultEngineParsePathJavaInfersEnhancedForReceiverType` (line 14)
- **Outer class context chain**:
  `java/java_call_context_test.go:TestDefaultEngineParsePathJavaAddsOuterClassContextToUnqualifiedCalls` (line 52)
- **Explicit outer this field receiver**:
  `java/java_call_context_test.go:TestDefaultEngineParsePathJavaInfersExplicitOuterThisFieldReceiver` (line 86)
- **Argument return type inference**:
  `java/java_call_context_test.go:TestDefaultEngineParsePathJavaInfersUnqualifiedArgumentReturnType` (line 117)
- **Literal reflection references (Class.forName, getMethod)**:
  `java/java_reflection_test.go:TestDefaultEngineParsePathJavaEmitsLiteralReflectionReferences` (line 14)
- **Dynamic reflection strings rejected**:
  `java/java_reflection_test.go:TestDefaultEngineParsePathJavaIgnoresDynamicReflectionStrings` (line 55)
- **Value-flow: gate off byte-identical**:
  `java/java_cfg_dataflow_test.go:TestJavaDataflowOffIsByteIdentical` (line 28)
- **Value-flow: taint Spring request param to JDBC sink**:
  `java/java_cfg_dataflow_test.go:TestJavaTaintSpringRequestParamToJDBCSink` (line 74)
- **Value-flow: wildcard imports to JDBC sink**:
  `java/java_cfg_dataflow_test.go:TestJavaTaintWildcardImportsToJDBCSink` (line 85)
- **Value-flow: try block JDBC sink**:
  `java/java_cfg_dataflow_test.go:TestJavaTaintTryBlockJDBCSink` (line 107)
- **Value-flow: same-named local annotation/sink ignored**:
  `java/java_cfg_dataflow_test.go:TestJavaTaintIgnoresSameNamedLocalAnnotationAndSink` (line 133)
- **Value-flow: interproc summaries and sources**:
  `java/java_cfg_dataflow_test.go:TestJavaInterprocSummariesAndSources` (line 154)
- **Value-flow: durable rows require package identity**:
  `java/java_cfg_dataflow_test.go:TestJavaDurableRowsRequirePackageIdentity` (line 201)
- **Serialization hooks**:
  `java/java_dead_code_serialization_roots_test.go:TestDefaultEngineParsePathJavaMarksSerializationRuntimeHooks` (line 14),
  `java/java_dead_code_serialization_roots_test.go:TestDefaultEngineParsePathJavaDoesNotRootOrdinaryMethodsWithHookNames` (line 75)
- **Lambda callback inference**:
  `java/java_dead_code_maturity_test.go:TestDefaultEngineParsePathJavaInfersTypedLambdaCallbackCalls` (line 14)
- **Gradle task setters and interface methods**:
  `java/java_dead_code_maturity_test.go:TestDefaultEngineParsePathJavaMarksGradleTaskSettersAndInterfaceMethods` (line 139)
- **Parameter annotations as decorators suppressed**:
  `java/java_dead_code_maturity_test.go:TestDefaultEngineParsePathJavaIgnoresParameterAnnotationsAsDecorators` (line 181)
- **Metadata: ServiceLoader providers**:
  `java/metadata_test.go:TestMetadataClassReferencesMetaInfServicesProvider`
- **Metadata: Spring AutoConfiguration.imports**:
  `java/metadata_test.go:TestMetadataClassReferencesSpringAutoconfigurationImports`
- **Metadata: invalid class names rejected**:
  `java/metadata_test.go:TestMetadataClassReferencesRejectsDynamicOrInvalidNames`,
  `java/metadata_test.go:TestMetadataClassReferencesRejectsBareSingleSegment`
- **Metadata: unrecognized paths**:
  `java/metadata_test.go:TestMetadataClassReferencesUnrecognizedPath`
- **Comprehensive dead-code fixture**:
  `java/java_dead_code_fixture_test.go:TestDefaultEngineParsePathJavaComprehensiveDeadCodeFixture` (line 14)
- **Comprehensive metadata fixture**:
  `java/java_dead_code_fixture_test.go:TestDefaultEngineParsePathJavaComprehensiveMetadataFixtures` (line 49)
- **Engine-level metadata dispatch**:
  `java/java_metadata_test.go:TestDefaultEngineParsePathJavaMetadataEmitsStaticClassReferences` (line 36)
- **Cyclomatic complexity**: tested in `engine_cyclomatic_complexity_test.go`

## Unverified / Claimed-but-Untested Constructs
- **Record declarations** in isolation: records are mentioned in
  `java/java_dead_code_maturity_test.go` (line 94) but tested alongside this-field
  receivers. No standalone record entity test verifying record-specific fields
  or compact constructor handling.
- **Sealed classes / permits clause** (Java 17+): not handled.
- **Pattern matching for switch** (Java 21+): not handled.
- **Text blocks**: multiline string literals may interact with source parsing
  but no test verifies.
- **`annotation_type_declaration` with decorators**: annotation types tested
  via `engine_managed_oo_test.go` but no dedicated verification of decorator
  propagation on annotation declarations.
- **`walkDirectNamed` helper**: tested indirectly only.
- **`javaAnnotationTargetKind`**: deducing annotation target (class vs method
  vs field) — no direct behavioral test.
- **`javaFirstTypeIdentifier` helpers**: fallback logic for unparseable call
  expression names, tested indirectly.
- **`javaTrailingIdentifier` for complex method references**: tested through
  existing method_reference tests but edge cases like generics in reference
  targets (`List<String>::size`) not explicitly verified.

## Edge Cases Considered
- Duplicate method reference targets not double-rooted:
  `java/java_dead_code_roots_test.go:TestDefaultEngineParsePathJavaDoesNotMarkDuplicateDeclaredTypeMethodReferenceTargets`
- Same-named local annotation as Spring annotation not confused:
  `java/java_cfg_dataflow_test.go:TestJavaTaintIgnoresSameNamedLocalAnnotationAndSink`
- Try-block JDBC sink still detected:
  `java/java_cfg_dataflow_test.go:TestJavaTaintTryBlockJDBCSink`
- Dynamic reflection strings not emitted as evidence:
  `java/java_reflection_test.go:TestDefaultEngineParsePathJavaIgnoresDynamicReflectionStrings`
- Ordinary methods with hook-like names not false-rooted:
  `java/java_dead_code_serialization_roots_test.go:TestDefaultEngineParsePathJavaDoesNotRootOrdinaryMethodsWithHookNames`
- Parameter annotations not treated as decorators:
  `java/java_dead_code_maturity_test.go:TestDefaultEngineParsePathJavaIgnoresParameterAnnotationsAsDecorators`
- Value-flow disabled by default; byte-identical output:
  `java/java_cfg_dataflow_test.go:TestJavaDataflowOffIsByteIdentical`
- Durable summary/source rows require package identity:
  `java/java_cfg_dataflow_test.go:TestJavaDurableRowsRequirePackageIdentity`
- Record and this-field receivers:
  `java/java_dead_code_maturity_test.go:TestDefaultEngineParsePathJavaModelsRecordsAndThisFieldReceivers`
- Interface, enum, and record method reference targets:
  `java/java_dead_code_roots_test.go:TestDefaultEngineParsePathJavaMarksInterfaceEnumAndRecordMethodReferenceTargets`
- Unqualified call class context chain:
  `java/java_call_context_test.go:TestDefaultEngineParsePathJavaAddsOuterClassContextToUnqualifiedCalls`
- Wildcard Spring imports accepted:
  `java/java_cfg_dataflow_test.go:TestJavaTaintWildcardImportsToJDBCSink`
- Metadata invalid class names, empty files, unrecognized paths rejected:
  `java/metadata_test.go`

## Edge Cases NOT Considered
- **Java module-info.java**: JPMS module declarations not tested.
- **Sealed class hierarchies** (Java 17+): no permits clause handling.
- **Record compact constructors**: records tested for field receivers but not
  compact constructor handling.
- **Pattern matching for instanceof** (Java 16+): no test.
- **Switch expressions** (Java 14+): no dedicated test.
- **Lambda parameter type inference** with generics: basic lambda callbacks
  tested but complex generic type inference not covered.
- **Annotation with array-valued elements** (e.g., `@RequestMapping({"/a", "/b"})`):
  not tested.
- **Nested annotation usage** (annotation on annotation declaration): not tested.
- **Multiple `spring.factories` continuations**: the backslash-line-continuation
  in `.properties` format is tested but deeply nested continuations may not be.
- **Null `methodReferences` index passed to `javaDeadCodeRootKinds`**: not tested.
- **Concurrent parse calls with shared `callInference` index**: not tested.

## Verdict
**deep**

The Java parser has the most extensive test coverage in Eshu: 41 external
`java_test` engine tests plus 12 in-package tests under `go/internal/parser/java`
(17 test files: 12 `java_*_test.go`, `engine_java_implements_test.go`, and the
four in-package files), with the parent's `engine_managed_oo_test.go` Java cases
on top, covering every entity type, all
15+ dead-code root kinds, call inference (receiver types, class context chains,
argument types), literal reflection evidence, ServiceLoader/Spring metadata, and
the full opt-in value-flow pipeline. The edge-case coverage is thorough for
false-positive suppression (duplicate roots, same-named locals, dynamic strings,
parameter annotations). Remaining gaps are primarily Java 14+ language features
and edge regression paths.

## Recommended Actions
1. Add standalone record declaration entity tests verifying record-specific
   fields (compact constructors, generated accessors if applicable).
2. Add a test for `sealed class` / `permits` clause handling (or document as
   intentionally unsupported with a skip-list test).
3. Add tests for switch expressions (Java 14+) and pattern matching for
   instanceof (Java 16+).
4. Add a test for annotation-type declarations with decorators and dead-code
   roots.
5. Add a test for `javaAnnotationTargetKind` covering all target parent kinds.
6. Add a benchmark/regression test for deep class nesting (outer -> inner ->
   inner-inner) to verify context chains don't blow up.
7. Add a test for `null` methodReferences index passed to
   `javaDeadCodeRootKinds`.
