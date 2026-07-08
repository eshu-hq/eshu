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
  `docs/public/reference/local-testing.md`

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
| Spring route entries | `spring-mvc-route-truth` | supported | `go/internal/parser/java_kotlin_spring_route_semantics_test.go::TestDefaultEngineParsePathKotlinSpringRouteSemantics` | Literal Spring MVC/WebFlux annotations emit exact `framework_semantics.spring.route_entries` with handler function names. `HANDLES_ROUTE` projection remains exact-only and skips ambiguous or unknown handlers. |
| JAX-RS route entries | `jax-rs-route-truth` | supported | `go/internal/parser/java_kotlin_spring_route_semantics_test.go::TestDefaultEngineParsePathKotlinJVMRouteSemantics` | Literal `@Path` plus HTTP method annotations emit exact `framework_semantics.jax_rs.route_entries` with handler function names. Dynamic paths are not guessed. |
| Micronaut route entries | `micronaut-route-truth` | supported | `go/internal/parser/java_kotlin_spring_route_semantics_test.go::TestDefaultEngineParsePathKotlinJVMRouteSemantics` | Literal `@Controller` plus HTTP method annotations emit exact `framework_semantics.micronaut.route_entries` with handler function names. Dynamic paths are not guessed. |
| Ktor route entries | `ktor-literal-handler-route-truth` | supported | `go/internal/parser/java_kotlin_spring_route_semantics_test.go::TestDefaultEngineParsePathKotlinJVMRouteSemantics` | Literal Ktor verb routes emit exact `framework_semantics.ktor.route_entries` only when the route lambda delegates to one exact bare handler function call. |

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

## Framework And Library Support

Supported today:

- Spring component and method callbacks, Gradle plugin/task callbacks, JUnit
  methods, lifecycle callbacks, and secondary constructors are modeled as
  derived roots.
- Literal Spring MVC/WebFlux `@RequestMapping`, `@GetMapping`, `@PostMapping`,
  `@PutMapping`, `@PatchMapping`, and `@DeleteMapping` annotations emit exact
  route entries when the path is source-literal. Class `@RequestMapping`
  literal prefixes and path variables are preserved; dynamic paths are not
  guessed.
- Literal JAX-RS `@Path` plus HTTP method annotations and Micronaut
  `@Controller` plus HTTP method annotations emit exact route entries when the
  path is source-literal and the handler is the annotated function.
- Literal Ktor verb routes emit exact route entries only when the route has one
  source-literal path and the route lambda delegates to exactly one bare
  handler function call.
- Interfaces, same-file interface implementations, overrides, and top-level
  `main` are modeled as root evidence.
- Maven/Gradle vulnerability reachability can use Kotlin imports, calls, and
  SCIP evidence only when resolver evidence proves the dependency's package API
  prefix; the result is reachable prioritization, not safe/not-called truth.

Not claimed today:

- Reflection, dependency injection, annotation processing, compiler plugins,
  Gradle source-set selection, multiplatform targets, and dynamic dispatch
  remain exactness blockers.
- Spring composed/meta-annotations, non-literal route paths, multi-route
  expansion policy, Ktor nested route-group prefixes, Ktor inline lambdas
  without a single exact handler function, external route configuration,
  runtime-discovered routes, generated handlers, and other JVM web frameworks
  are not claimed as exact route truth.

## Parser Performance

The Kotlin parser collapses its four framework-route detectors — Spring,
JAX-RS, Micronaut, and Ktor — from four independent full-tree tree-sitter
walks into one combined walk. Each detector only reads its own node kind
(`function_declaration` for Spring/JAX-RS/Micronaut, `call_expression` for
Ktor) and writes to its own route slice, with no shared mutable state and no
dependency on another detector's output, so they collapse safely. The
`collectDeclarations` pre-pass and the main declaration/call walk are
unaffected: `collectDeclarations` seeds type information the main walk
consumes and must still run first. This lowers the framework-detection walk
count from 4 to 1 while keeping parser output byte-identical, verified by a
one-time old-vs-new `0/0` symmetric-diff over the fixture corpus via the
opt-in `KOTLIN_PARSE_DUMP` harness (`equivalence_dump_test.go`, a manual
differential — not a standing CI gate); standing regression protection comes
from the Kotlin parser package tests and the B-12 golden snapshot (epic
#4831, #4841). Contributors adding a new framework-route detector should
extend the shared pass rather than add another full-tree walk when the
detector has no dependency on another detector's completed output.
