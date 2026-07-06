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
- The parser performs three full-tree AST traversals per file (parent-edge
  index, declaration/import/type-evidence/dead-code/route-attribute
  collection, then variable/call emission). Symfony route-attribute
  resolution folded into the declaration pass instead of running its own
  traversal (#4515); emitted payload shape is unchanged. See
  `go/internal/parser/php/README.md` for the walk-count and byte-identity
  evidence.

## Framework And Library Support

Supported today:

- Route-backed controller actions, literal route handlers, Symfony route
  attributes, and WordPress hook callbacks are modeled as derived roots.
- Method-level attributes resolved to Symfony `Route` also emit exact
  `framework_semantics.symfony.route_entries` when the path and methods are
  literal and the declaring method is the handler. `HANDLES_ROUTE` projection
  still requires an exact reducer match to that class-qualified method.
- Constructors, magic methods, same-file interface methods and implementations,
  and trait methods are also modeled as live root evidence.

Not claimed today:

- Composer/autoload public surfaces, Laravel router conventions, Symfony
  dynamic attributes, broader framework route resolution, include/require
  resolution, reflection-heavy flows, and arbitrary dynamic dispatch remain
  outside the exactness boundary.
- Exact route-to-handler truth for Laravel, WordPress, broader Symfony, and
  other PHP frameworks is tracked by
  [#4162](https://github.com/eshu-hq/eshu/issues/4162).

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
  dropped. The repository pre-scan stage is not yet bounded by this cap and
  can still fully parse an over-cap file at the same cost; tracked in
  [#4808](https://github.com/eshu-hq/eshu/issues/4808).
