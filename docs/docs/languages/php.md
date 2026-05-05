# PHP Parser

This page tracks the checked-in Go PHP parser and query contract in the current repository state.

Canonical implementation:
- Parser: `go/internal/parser/php_language.go`
- Registry: `go/internal/parser/registry.go`
- Query proof: `go/internal/query/*php*`
- Fixture repo: `tests/fixtures/ecosystems/php_comprehensive/`

## Parser Contract

- Language: `php`
- Family: `language`
- Parser: `DefaultEngine (php)`
- Integration validation: compose-backed fixture verification via
  `docs/docs/reference/local-testing.md`

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

## Current Truth

- The current Go parser covers the documented PHP object-call and aliasing
  families end to end.
- The public Go `code/relationships` surface now has checked-in proof for the
  bounded PHP receiver families covered on this page.
- Remaining PHP work, if any, is net-new future enhancement work around fully
  dynamic dispatch and reflection-heavy flows beyond the documented contract.

## Known Limitations

- Trait adaptation semantics beyond the bounded alias and override paths remain
  intentionally narrow.
- Fully dynamic PHP dispatch, reflection-heavy call sites, and arbitrary
  whole-program alias flow remain bounded future work beyond the documented
  contract.
