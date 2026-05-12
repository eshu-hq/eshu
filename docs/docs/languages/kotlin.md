# Kotlin Parser

This page tracks the checked-in Go Kotlin parser and query contract in the current repository state.

Canonical implementation:
- Parser: `go/internal/parser/kotlin_language.go`
- Registry: `go/internal/parser/registry.go`
- Query proof: `go/internal/query/*kotlin*`
- Fixture repo: `tests/fixtures/ecosystems/kotlin_comprehensive/`
- Dead-code fixture repo: `tests/fixtures/deadcode/kotlin/`

## Parser Contract

- Language: `kotlin`
- Family: `language`
- Parser: `DefaultEngine (kotlin)`
- Integration validation: compose-backed fixture verification via
  `docs/docs/reference/local-testing.md`

## Capability Checklist

| Capability | ID | Status | Evidence | Current truth |
| --- | --- | --- | --- | --- |
| Core declarations | `core-declarations` | supported | `go/internal/parser/engine_long_tail_test.go::TestDefaultEngineParsePathKotlinFixtures` | Functions, classes, objects, companion objects, imports, and properties all parse natively in Go. |
| Extension receiver tracking | `extension-receiver-tracking` | supported | `go/internal/parser/engine_kotlin_call_metadata_test.go::TestDefaultEngineParsePathKotlinInfersLocalReceiverTypesForDotCalls` | Extension receiver type stays attached to function metadata so reducer and query layers can resolve receiver-qualified calls. |
| Suspend functions | `suspend-function-semantics` | supported | `go/internal/parser/engine_kotlin_suspend_test.go::TestDefaultEngineParsePathKotlinMarksSuspendFunctions`, `go/internal/reducer/code_call_materialization_kotlin_suspend_test.go::TestExtractCodeCallRowsResolvesKotlinSuspendFunctionCallsUsingInferredObjectType` | Suspend declarations keep `suspend: true` through parser and reducer materialization. |
| Receiver inference | `receiver-inference` | supported | `go/internal/parser/engine_kotlin_call_metadata_test.go::TestDefaultEngineParsePathKotlinInfersLocalReceiverTypesForDotCalls`, `go/internal/reducer/code_call_materialization_kotlin_test.go::TestExtractCodeCallRowsResolvesKotlinTypedReceiverCallsUsingInferredObjectType`, `go/internal/query/code_relationships_graph_kotlin_php_receivers_test.go::TestHandleRelationshipsReturnsGraphBackedKotlinLocalTypedReceiverCalls` | Typed locals, casts, direct cast expressions, object receivers, companion-object receivers, typed infix, and primary-constructor properties now materialize canonical Kotlin `CALLS` edges and have graph-backed public query proof. |
| Smart casts and safe calls | `smart-casts-and-safe-calls` | supported | `go/internal/parser/engine_kotlin_smart_cast_test.go::TestDefaultEngineParsePathKotlinInfersIfSmartCastReceiverTypesForDotCalls`, `go/internal/reducer/code_call_materialization_kotlin_smart_cast_test.go::TestExtractCodeCallRowsResolvesKotlinGenericSmartCastReceiverChainsUsingInferredObjectType`, `go/internal/query/code_relationships_graph_kotlin_php_long_tail_test.go::TestHandleRelationshipsReturnsGraphBackedKotlinGenericSmartCastReceiverChains` | `if`/`when` smart casts, generic smart casts, safe-call receiver chains, and safe-call alias chains survive parser inference, reducer materialization, and public `code/relationships` proof. |
| Scope-function preservation | `scope-function-preservation` | supported | `go/internal/parser/engine_kotlin_scope_function_test.go::TestDefaultEngineParsePathKotlinInfersAlsoScopeFunctionPreservedAssignmentReceiverTypesForDotCalls`, `go/internal/reducer/code_call_materialization_kotlin_scope_function_test.go::TestExtractCodeCallRowsResolvesKotlinAlsoScopeFunctionPreservedAssignmentReceiverCallsUsingInferredObjectType`, `go/internal/query/code_relationships_graph_kotlin_php_receivers_test.go::TestHandleRelationshipsReturnsGraphBackedKotlinAlsoScopeFunctionPreservedAssignmentReceiverCalls` | Receiver-preserving `apply` and `also` assignment flows plus direct scope-function result chains keep receiver type strongly enough to materialize canonical edges. |
| Lazy delegated properties | `delegated-lazy-properties` | supported | `go/internal/parser/engine_kotlin_lazy_property_test.go::TestDefaultEngineParsePathKotlinInfersLazyDelegatedPropertyReceiverTypesForDotCalls`, `go/internal/query/code_relationships_graph_test.go::TestHandleRelationshipsReturnsGraphBackedKotlinLazyDelegatedPropertyReceiverCalls` | `by lazy { ... }` receivers survive parser, reducer, and graph-backed query proof, including `call_kind` propagation. |
| Same-file function-return aliasing | `same-file-function-return-aliasing` | supported | `go/internal/parser/engine_kotlin_function_return_alias_test.go::TestDefaultEngineParsePathKotlinInfersSameFileFunctionReturnTypeAliasCalls`, `go/internal/reducer/code_call_materialization_kotlin_function_return_alias_test.go::TestExtractCodeCallRowsResolvesKotlinSameFileFunctionReturnAliasChainCallsUsingInferredObjectType`, `go/internal/query/code_relationships_graph_kotlin_function_returns_test.go::TestHandleRelationshipsReturnsGraphBackedKotlinSameFileFunctionReturnAliasChainCalls` | Same-file function-return aliases and re-aliased local chains survive the full parser/reducer/query path. |
| Cross-file function-return chaining | `cross-file-function-return-chaining` | supported | `go/internal/parser/engine_kotlin_function_return_alias_test.go::TestDefaultEngineParsePathKotlinInfersCrossFilePackageAwareFunctionReturnReceiverChainsForDotCalls`, `go/internal/reducer/code_call_materialization_kotlin_function_return_receiver_chain_test.go::TestExtractCodeCallRowsResolvesKotlinCrossFilePackageAwareFunctionReturnReceiverChainsUsingInferredObjectType`, `go/internal/query/code_relationships_graph_kotlin_package_returns_test.go::TestHandleRelationshipsReturnsGraphBackedKotlinCrossFilePackageAwareFunctionReturnReceiverChains` | Sibling-file, parent-directory, parenthesized, and package-aware cross-file function-return chains all materialize canonical edges and have public query proof. |
| Constructor-root receiver chains | `constructor-root-receiver-chains` | supported | `go/internal/parser/engine_kotlin_function_return_alias_test.go::TestDefaultEngineParsePathKotlinInfersConstructorRootReceiverChainsForDotCalls`, `go/internal/query/code_relationships_graph_kotlin_php_test.go::TestHandleRelationshipsReturnsGraphBackedKotlinConstructorRootReceiverChains` | Constructor-root and parenthesized receiver chains keep the correct call-site semantics instead of collapsing into declaration-line noise. |
| Class and interface context | `class-context-on-functions` | supported | `go/internal/parser/engine_kotlin_interface_test.go::TestDefaultEngineParsePathKotlinInterfaceMembersCarryTypeContext`, `go/internal/reducer/code_call_materialization_kotlin_interface_test.go::TestExtractCodeCallRowsResolvesKotlinInterfaceTypedReceiverCallsUsingInferredObjectType` | Class and interface methods carry `class_context`, which keeps interface-typed receiver calls resolvable on the normal reducer/query path. |
| Secondary constructors | `secondary-constructors` | supported | `go/internal/parser/engine_managed_oo_test.go::TestDefaultEngineParsePathKotlinSecondaryConstructors`, `go/internal/query/entity_story_kotlin_test.go::TestAttachSemanticSummaryAddsKotlinSecondaryConstructorStory` | Secondary constructors keep `constructor_kind` metadata through semantic summaries and stories. |
| Dead-code roots | `dead-code-derived-roots` | supported | `go/internal/parser/kotlin_dead_code_roots_test.go::TestDefaultEngineParsePathKotlinEmitsDeadCodeRootKinds`, `go/internal/query/code_dead_code_kotlin_roots_test.go::TestHandleDeadCodeExcludesKotlinRootKindsFromMetadata` | Parser metadata marks top-level `main`, secondary constructors, interface methods, same-file interface implementations, overrides, Gradle plugin/task callbacks, Spring component and method callbacks, lifecycle callbacks, and JUnit methods as `kotlin.*` dead-code roots. The query layer suppresses those parser-backed roots before returning cleanup candidates. |

## Current Truth

- The current Go parser covers the documented Kotlin receiver and call families
  end to end.
- The public Go `code/relationships` surface has checked-in proof for the
  Kotlin long-tail receiver families described on this page.
- `code_quality.dead_code` reports Kotlin as `derived`. Exact cleanup remains
  blocked by reflection, dependency injection, annotation processing, compiler
  plugin output, dynamic dispatch, Gradle source-set resolution, Kotlin
  multiplatform target resolution, and broad public API surface resolution.

## Known Limitations

- Kotlin interfaces are separately bucketed in the parser, but interface
  methods now carry `class_context` so interface-typed receiver calls still
  resolve through the normal reducer/query path.
- Fully general whole-program data-flow inference remains intentionally bounded.
  The shipped Go path already covers the documented receiver surface: typed
  locals, casts, smart casts, safe calls, scope-function-preserved assignments,
  lazy delegates, and package-aware function-return chains.
- Kotlin script and Gradle source-set selection are not resolved as exact
  dead-code scope boundaries. They are named exactness blockers rather than
  hidden assumptions.
