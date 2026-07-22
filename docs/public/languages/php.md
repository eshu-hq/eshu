# PHP Parser

This page tracks the checked-in Go PHP parser and query contract in the current repository state.

Canonical implementation:
- Parser: `go/internal/parser/php/`
- Registry: `go/internal/parser/registry.go`
- Query proof: `go/internal/query/*php*`
- Fixture repo: `tests/fixtures/ecosystems/php_comprehensive/`

## Parser Contract

- Language: `php`
- Family: `language`
- Parser: `DefaultEngine (php)`
- Integration validation: compose-backed fixture verification via
  `docs/public/reference/local-testing.md`

## Capability Checklist

| Capability | ID | Status | Evidence | Current truth |
| --- | --- | --- | --- | --- |
| Core declarations | `core-declarations` | supported | `go/internal/parser/php_language_test.go::TestDefaultEngineParsePathPHPEmitsFunctionParametersSourceAndContext`, `go/internal/parser/php_language_test.go::TestDefaultEngineParsePathPHPEmitsInheritanceAndImportMetadata` | Functions, methods, classes, interfaces, traits, variables, and grouped `use` declarations parse natively in Go. |
| Trait adaptation aliases | `trait-adaptation-aliases` | supported | `go/internal/parser/php_language_trait_adaptation_test.go::TestDefaultEngineParsePathPHPEmitsTraitAdaptationMetadata`, `go/internal/reducer/inheritance_php_trait_adaptations_test.go::TestExtractInheritanceRowsMaterializesPHPTraitAdaptationOverrides`, `go/internal/query/code_relationships_graph_test.go::TestHandleRelationshipsReturnsGraphBackedPHPTraitMethodAliases` | Trait `insteadof` and `as` clauses materialize `OVERRIDES` plus class-level and method-level `ALIASES` edges on the normal Go path. |
| Static receiver families | `static-receiver-families` | supported | `go/internal/parser/php_language_static_property_receiver_test.go::TestDefaultEngineParsePathPHPInfersParentAndStaticPropertyReceiverChains`, `go/internal/reducer/code_call_materialization_php_static_property_receiver_test.go::TestExtractCodeCallRowsResolvesPHPParentAndStaticPropertyReceiverChainsUsingTypedPropertyInference`, `go/internal/query/code_relationships_graph_kotlin_php_additional_test.go::TestHandleRelationshipsReturnsGraphBackedPHPParentAndStaticPropertyReceiverAccessChains` | Direct static calls, static-property receiver chains, parent/static property access chains, imported static alias chains, and direct `self`/`static` instantiation rows all materialize canonical graph edges and have public query proof. |
| Typed property and alias receivers | `typed-property-and-alias-receivers` | supported | `go/internal/parser/php_language_alias_test.go::TestDefaultEngineParsePathPHPInfersAliasedNewExpressionReceiverCalls`, `go/internal/reducer/code_call_materialization_family_test.go::TestExtractCodeCallRowsResolvesPHPPropertyChainAliasCallsUsingTypedPropertyInference`, `go/internal/query/code_relationships_graph_kotlin_php_receivers_test.go::TestHandleRelationshipsReturnsGraphBackedPHPAliasedNewExpressionReceiverCalls` | Typed `$this` receivers, aliased `new` expressions, imported class aliases, and property-chain aliases all survive parser inference, reducer materialization, and graph-backed public query proof. |
| Function-return receiver chains | `function-return-receiver-chains` | supported | `go/internal/parser/php_language_function_chain_test.go::TestDefaultEngineParsePathPHPInfersFreeFunctionReturnCallChainReceiverCalls`, `go/internal/reducer/code_call_materialization_php_function_receiver_chain_test.go::TestExtractCodeCallRowsResolvesPHPFreeFunctionReturnCallChainReceiverCallsUsingTypedPropertyInference`, `go/internal/query/code_relationships_graph_php_long_tail_test.go::TestHandleRelationshipsReturnsGraphBackedPHPFreeFunctionReturnCallChainReceiverCalls` | Same-file free-function return aliases, direct receiver chains, and return call chains all materialize canonical object-call edges on the Go path. |
| Method-return receiver chains | `method-return-receiver-chains` | supported | `go/internal/parser/php_language_method_chain_test.go::TestDefaultEngineParsePathPHPInfersMethodReturnPropertyDereferenceReceiverCalls`, `go/internal/reducer/code_call_materialization_php_method_return_chain_test.go::TestExtractCodeCallRowsResolvesPHPMethodReturnPropertyDereferenceReceiverCallsUsingTypedPropertyInference`, `go/internal/query/code_relationships_graph_kotlin_php_additional_test.go::TestHandleRelationshipsReturnsGraphBackedPHPSameFileMethodReturnPropertyChainAliasCalls` | Method-return call chains, property dereference chains, and parenthesized method-return chains survive parser inference, reducer materialization, and graph-backed public query proof. |
| Cross-file object-call families | `cross-file-object-call-families` | supported | `go/internal/reducer/code_call_materialization_cross_file_exact_test.go::TestExtractCodeCallRowsResolvesCrossFilePHPMethodReturnCallChainReceiverCallsUsingTypedPropertyInference`, `go/internal/query/code_relationships_graph_kotlin_php_test.go::TestHandleRelationshipsReturnsGraphBackedPHPCrossFileReturnTypeAliasedCalls`, `go/internal/query/code_relationships_graph_php_long_tail_test.go::TestHandleRelationshipsReturnsGraphBackedPHPCrossFileChainedStaticFactoryReturnCalls` | Cross-file return-type aliases, cross-file method-return chains, and cross-file chained static factory returns are all query-proven in the current platform. |
| Nullsafe and anonymous-class receivers | `nullsafe-and-anonymous-class-receivers` | supported | `go/internal/parser/php_language_test.go::TestDefaultEngineParsePathPHPEmitsNullsafeReceiverMetadata`, `go/internal/reducer/code_call_materialization_family_test.go::TestExtractCodeCallRowsResolvesPHPNullsafeReceiverChainsUsingTypedPropertyInference`, `go/internal/query/code_relationships_graph_php_long_tail_test.go::TestHandleRelationshipsReturnsGraphBackedPHPAnonymousClassReceiverCalls` | Nullsafe receiver chains and anonymous-class receiver calls both survive the full parser/reducer/query path. |
| Symfony attribute route truth | `symfony-attribute-route-truth` | supported | `go/internal/parser/php_route_entries_test.go::TestDefaultEngineParsePathPHPEmitsSymfonyRouteEntries`, `go/internal/reducer/handles_route_php_test.go::TestBuildHandlesRouteIntentRowsEmitsPHPSymfonyRouteMatches`, `go/internal/query/content_reader_framework_routes_php_test.go::TestParseFrameworkSemanticsExtractsPHPSymfonyRoutes` | Method-level attributes resolved to Symfony `Route` emit exact `framework_semantics.symfony.route_entries` when the source proves literal path, literal HTTP method list, and declaring handler method. `HANDLES_ROUTE` is projected only when the reducer resolves that class-qualified handler exactly. |
| Slim web-route detection | `slim-web-route-detection` | supported | `go/internal/parser/php_route_entries_test.go::TestDefaultEngineParsePathPHPEmitsSlimRouteEntries`, `go/internal/reducer/handles_route_php_test.go::TestBuildHandlesRouteIntentRowsEmitsPHPSlimRouteMatches`, `go/internal/parser/php_route_entries_test.go::TestDefaultEngineParsePathPHPEmitsSlimGroupedAndNestedRouteEntries`, `go/internal/parser/php_route_entries_test.go::TestDefaultEngineParsePathPHPSkipsNonSlimReceiverGetCalls` | Slim `$app->get()`, `$app->post()`, `$app->map()` and related route-registration calls emit `framework_semantics.slim.route_entries` when the receiver is a proven Slim app/group (a variable assigned from `AppFactory::create()`/`new \Slim\App(...)`, or a closure parameter typed `App`/`RouteCollectorProxy`/`RouteCollectorProxyInterface`/`RouteCollectorInterface`) and the first argument is a literal path string. A Slim import alone is not sufficient, so non-route calls like `$container->get('settings')` are not emitted. Phase-1 `member_call_expression` gathering + post-walk receiver-gated resolution; no dedicated route walk. Group-prefix concatenation is implemented: inner routes under `$app->group('/users', function ($group) { ... })` (including nested groups) emit the full prefixed path (`/users`, `/users/{id}`). |
| Laravel Route:: facade route truth | `laravel-route-facade-truth` | supported | `go/internal/parser/php_route_entries_test.go::TestDefaultEngineParsePathPHPEmitsLaravelRouteEntries`, `go/internal/parser/php_route_entries_test.go::TestDefaultEngineParsePathPHPSkipsNonLaravelScopedGetCall`, `go/internal/parser/php_route_entries_test.go::TestDefaultEngineParsePathPHPEmitsLaravelNestedGroupRouteEntries`, `go/internal/parser/php_route_entries_test.go::TestDefaultEngineParsePathPHPEmitsLaravelGlobalBackslashRouteInNamespace`, `go/internal/reducer/handles_route_php_test.go::TestBuildHandlesRouteIntentRowsEmitsPHPLaravelAtJoinedRouteMatches`, `go/internal/query/route_query_proof_matrix_test.go::TestRouteQueryProofMatrix` | Laravel `Route::get()`, `Route::post()`, `Route::put()`, `Route::patch()`, `Route::delete()`, `Route::options()`, `Route::any()`, and `Route::match()` scoped calls emit exact `framework_semantics.laravel.route_entries` when the scope resolves to the `Illuminate\Support\Facades\Route` facade. Idiomatic `Controller@method` string-callable handlers resolve through a bounded normalization to the existing class-qualified dotted candidate, so `UserController@index` projects `HANDLES_ROUTE` and is returned by `trace_route_callers`. Resolution remains exact-only: it never falls back to the bare method, and a wrong or ambiguous controller emits no edge. `Route::resource()` expansion and non-literal group prefixes remain deferred. |
| Dead-code root hints | `dead-code-root-hints` | derived | `go/internal/parser/php_dead_code_roots_test.go::TestDefaultEngineParsePathPHPEmitsDeadCodeRootKinds`, `go/internal/query/code_dead_code_php_roots_test.go::TestHandleDeadCodeExcludesPHPRootKindsFromMetadata`, `tests/fixtures/deadcode/php/app.php` | Parser metadata suppresses PHP script entrypoints, constructors, known PHP magic methods, same-file interface methods and implementations, trait methods, route-backed controller actions, literal route handlers, Symfony route attributes, and WordPress hook callbacks from cleanup candidates. |

## Current Truth

- The Go parser covers the documented PHP object-call and aliasing families end
  to end.
- Literal method-level Symfony `Route` attributes emit exact route entries and
  can project exact `HANDLES_ROUTE` edges when the handler method resolves.
- The public Go `code/relationships` surface has checked-in proof for the
  bounded PHP receiver families covered on this page.
- `code_quality.dead_code` reports PHP as `derived`, not exact. The current
  root model is parser-backed and bounded to same-file declarations plus
  literal route, attribute, and hook registrations.
- Fully dynamic dispatch and reflection-heavy flows remain outside the
  documented contract.
- The parser performs two full-tree AST traversals per file (parent-edge
  index plus one phase-1 `shared.WalkNamed` that collects declarations,
  imports, type evidence, dead-code facts, route-attribute candidates, and
  phase-2 resolution-candidate node pointers). Phase-2 variable and call
  resolution runs in-memory over the pre-gathered slices instead of
  re-walking the full tree (#4923). Symfony route-attribute resolution folded
  into the declaration pass instead of running its own traversal (#4515).
  Emitted payload shape is unchanged (0/0 symmetric diff). See
  `go/internal/parser/php/README.md` for the walk-count and byte-identity
  evidence.

## Parser Performance

Performance Evidence: The phase-2 `shared.WalkNamed` re-walk (six node-kind
switch over `variable_name`, `member_call_expression`, `nullsafe_member_call_expression`,
`scoped_call_expression`, `object_creation_expression`, and `function_call_expression`)
was replaced with in-memory `for` loops over pre-gathered node slices collected
during phase 1 (issue #4923, epic #4917). The winning Go child (#4921) set the
pattern; theory-proof microbench over the php_regression fixture padded to
≥10K LOC returned: OLD (phase-2 WalkNamed) 18,435,530 ns/op, 3,571,107 B/op,
139,565 allocs/op vs NEW (in-memory loops) 363,492 ns/op, 151,764 B/op,
7,588 allocs/op (~50.7× speedup on the isolated re-walk step).
BenchmarkParse/php (the real engine-level benchmark) confirmed the win at
production scale: sec/op −12.43% (164.3ms → 143.9ms, p=0.000, n=10),
B/op −7.82% (36.01Mi → 33.19Mi, p=0.000), allocs/op −11.92%
(1,043.1k → 918.8k, p=0.000). Output equivalence is 0/0 symmetric diff
over the full fixture corpus via the opt-in PHP_PARSE_DUMP harness
(equivalence_dump_test.go, a manual differential — not a standing CI gate);
standing regression protection comes from the PHP parser package tests and
the B-12 golden snapshot.

No-Observability-Change: this parser package emits no metrics, spans, or logs.
Operators continue to diagnose parser cost through collector snapshot stage
logs and `eshu_dp_file_parse_duration_seconds`.

- The dead-code root scan's Laravel-style literal route-target extraction
  (`[Controller::class, 'action']` array literals, `go/internal/parser/php/dead_code_roots.go`)
  used to call `shared.WalkNamed` a second time on each array element from
  inside the main declaration walk's callback, redundantly re-traversing a
  subtree the outer walk had already visited. `phpClassConstantClassName` and
  `phpStringLiteralValue` now search the same subtree, in the same pre-order,
  through a purpose-built early-exit-capable helper (`phpFirstNamedMatch`)
  instead of `shared.WalkNamed`. This removes `shared.WalkNamed`'s per-call
  closure and test-hook overhead and stops the search the instant a match is
  found, rather than always finishing the whole subtree. It does **not**
  narrow the search depth: the helper still finds a `Class::class` constant or
  string literal wrapped in parentheses, casts, or a ternary's branches one
  level below the array element's direct child, exactly as the old
  `shared.WalkNamed` call did, so a 500-route microbenchmark shows about a 2x
  time and 25% allocation reduction with no change in which node matches
  (epic #4831, #4844).
- Output is byte-identical, verified by a one-time old-vs-new `0/0`
  symmetric-diff over the fixture corpus via the opt-in `PHP_PARSE_DUMP`
  harness (`equivalence_dump_test.go`, a manual differential — not a standing
  CI gate); standing regression protection comes from the PHP parser package
  tests (including `TestPHPClassMethodArrayHandlesWrappedClassConstant`,
  which locks in the wrapped-expression equivalence the existing fixture
  corpus does not exercise) and the B-12 golden snapshot.

## Framework And Library Support

Supported today:

- Route-backed controller actions, literal route handlers, Symfony route
  attributes, and WordPress hook callbacks are modeled as derived roots.
- Method-level attributes resolved to Symfony `Route` also emit exact
  `framework_semantics.symfony.route_entries` when the path and methods are
  literal and the declaring method is the handler. `HANDLES_ROUTE` projection
  still requires an exact reducer match to that class-qualified method.
- Slim `$app->get()`/`$app->post()`/`$app->map()` and related route-registration
  calls emit `framework_semantics.slim.route_entries` when the file imports a
  Slim namespace. Detection is phase-1 `member_call_expression` candidate
  gathering gated on a Slim `use` import, with no dedicated second tree walk.
  `$app->group()` prefix concatenation is implemented: inner routes registered
  under a group (including nested groups) emit the full prefixed path.
- Laravel `Route::get()`, `Route::post()`, `Route::match()`, and related scoped
  call expressions emit `framework_semantics.laravel.route_entries` when the
  scope resolves to the `Illuminate\Support\Facades\Route` facade. Detection is
  phase-1 `scoped_call_expression` candidate gathering gated on Laravel route
  verbs and post-walk facade-provenance resolution. `Route::group(['prefix'
  => ...])` prefix concatenation is implemented for literal prefix values
  (nested groups supported).
- Constructors, magic methods, same-file interface methods and implementations,
  and trait methods are also modeled as live root evidence.

Not claimed today:

- Symfony dynamic attributes, broader framework route resolution,
  include/require resolution, reflection-heavy flows, and arbitrary dynamic
  dispatch remain outside the exactness boundary.
- Composer/autoload public surfaces remain outside the exactness boundary.
- Laravel `Route::resource()` expansion and non-literal group prefixes are
  deferred. Literal `Controller@method` string callables are supported through
  exact short-class-qualified resolution. Fully-qualified
  `Namespace\Controller@method` tokens remain unresolved until the parser
  carries per-declaration namespace evidence; shortening them to
  `Controller.method` would permit cross-namespace false matches. Dynamic
  callables remain unclaimed.

## Known Limitations

- Trait adaptation semantics beyond the bounded alias and override paths remain
  intentionally narrow.
- Fully dynamic PHP dispatch, reflection-heavy call sites, Composer/autoload
  public surfaces, include/require resolution, namespace alias breadth, broader
  framework route resolution, and arbitrary whole-program alias flow remain
  outside the documented contract.
- A PHP file larger than 1 MiB has its tree-sitter parse skipped entirely in
  the normal parse stage, to bound superlinear tree-sitter parse cost on very
  large generated files such as CID font maps or bundled library sources
  (#4766). No entities are extracted from a bounded file. The bound is
  recorded in `payload["php_parse_bounded"]` and logged, never silently
  dropped. The repository pre-scan stage is bounded by the same cap
  ([#4808](https://github.com/eshu-hq/eshu/issues/4808)): a bounded file
  contributes no pre-scan names and the bound is logged, since pre-scan has
  no payload map to carry a `php_parse_bounded` row.
