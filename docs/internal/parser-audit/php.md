# PHP Parser Audit

## Overview
The PHP parser (`go/internal/parser/php/`) is a tree-sitter-based adapter that walks the PHP AST in two passes. Pass 1 collects declarations, imports, property/return-type evidence, and dead-code facts. Pass 2 emits variable rows and call rows with inferred receiver types. It emits seven payload buckets (functions, classes, traits, interfaces, variables, imports, function_calls) plus namespace metadata, trait adaptations, semantic kind classification, and cyclomatic complexity. Dead-code root classification covers 10 bounded same-file root kinds. The parser depends on `internal/parser/shared` and the tree-sitter PHP grammar; it does not import the parent parser package.

## Claimed Constructs
Constructs the parser claims to extract, with source-file references.

| # | Construct | Source |
|---|---|---|
| 1 | Classes (with bases: extends, implements, trait uses) | `parser.go:130`, `declarations.go:15-33`, `support.go:15-21` |
| 2 | Interfaces (with extends bases) | `parser.go:131`, `declarations.go:38-50`, `support.go:24-26` |
| 3 | Traits | `parser.go:133`, `declarations.go:54-62` |
| 4 | Anonymous classes (synthetic name, bases, parent type) | `parser.go:136`, `declarations.go:67-83`, `anonymous.go:8-10` |
| 5 | Functions / methods (params, return type, source, semantic kind) | `parser.go:137`, `declarations.go:134-183`, `support.go:247-252` |
| 6 | Imports (single `use`, grouped `use`, `use function`, `use const`, aliases) | `parser.go:128`, `imports.go:16-44` |
| 7 | Variables (with type inference from properties, parameters, assignments) | `parser.go:155`, `variable_emit.go:17-67` |
| 8 | Instance member calls (`->`) | `parser.go:157`, `call_emit.go:84-104` |
| 9 | Nullsafe calls (`?->`) | `parser.go:157` (same handler as member calls), `call_emit.go:249-251` |
| 10 | Scoped/static calls (`::`) | `parser.go:159`, `call_emit.go:109-136` |
| 11 | Object creation calls (`new X()`) | `parser.go:161`, `call_emit.go:140-157` |
| 12 | Free function calls (`foo()`) | `parser.go:163`, `call_emit.go:162-177` |
| 13 | Namespace extraction | `parser.go:97-99`, `parser.go:181-198` |
| 14 | Trait adaptations (`insteadof`, `as`) | `trait_adaptation.go:15-31` |
| 15 | Receiver type inference ($this, parameters, new, self/static/parent, return types, property chains, static properties) | `alias.go:10-78` |
| 16 | Parenthesized receiver normalization | `returns.go:62-76` |
| 17 | Variable assignment RHS type inference | `variable_emit.go:102-139` |
| 18 | Constructor property promotion (typed) | `declarations.go:199-221` |
| 19 | Cyclomatic complexity (McCabe) | `complexity.go:17-48` |
| 20 | Dead-code root kinds (10 kinds) | `dead_code_roots.go:270-316` |
| 21 | Call deduplication (full_name + line) | `calls.go:28-30` |
| 22 | Variable deduplication (by `$name`) | `variable_emit.go:46-49` |

## Verified-by-Test Constructs
Constructs verified by at least one test with file:function references.

| Construct | Test file:function |
|---|---|
| Functions (parameters, source, class_context) | `php_language_test.go:TestDefaultEngineParsePathPHPEmitsFunctionParametersSourceAndContext` |
| Classes (bases, inheritance) | `php_language_test.go:TestDefaultEngineParsePathPHPEmitsInheritanceAndImportMetadata` |
| Interfaces (bases) | `php_language_test.go:TestDefaultEngineParsePathPHPEmitsInheritanceAndImportMetadata` |
| Traits (bucket, functions wired to trait) | `engine_long_tail_test.go:TestDefaultEngineParsePathPHPFixtures` (traits subtest, line 111-117) |
| Anonymous classes (name, bases, variable type, call inference) | `php_language_anonymous_test.go:TestDefaultEngineParsePathPHPEmitsAnonymousClassMetadata` |
| Imports (single use, alias, full_import_name, is_dependency, no-alias case) | `php_language_test.go:TestDefaultEngineParsePathPHPEmitsInheritanceAndImportMetadata` |
| Grouped use imports (aliases) | `php_language_test.go:TestDefaultEngineParsePathPHPEmitsGroupedUseImportMetadata` |
| `use function` / `use const` import kinds | `php_language_test.go:TestDefaultEngineParsePathPHPEmitsGroupedUseFunctionAndConstImportKinds` |
| Variables (context, class_context, type from `new`, typed properties) | `php_language_test.go:TestDefaultEngineParsePathPHPEmitsVariableAndCallMetadata`, `TestDefaultEngineParsePathPHPEmitsPropertyTypeInferenceFromDeclaration` |
| Instance member calls (full_name, args, context, class_context) | `php_language_test.go:TestDefaultEngineParsePathPHPEmitsVariableAndCallMetadata` |
| Nullsafe calls (normalized to `->`, chained inference) | `php_language_test.go:TestDefaultEngineParsePathPHPEmitsNullsafeReceiverMetadata` |
| Scoped calls (Logger::warn, namespaced variant) | `php_language_test.go:TestDefaultEngineParsePathPHPEmitsStaticMethodReceiverMetadata` |
| Object creation (`new Service()`) | `php_language_test.go:TestDefaultEngineParsePathPHPEmitsVariableAndCallMetadata` |
| Free function calls (`greet()`) | `php_language_test.go:TestDefaultEngineParsePathPHPEmitsCallContextLineMetadata` |
| Magic method classification (`semantic_kind`) | `php_language_test.go:TestDefaultEngineParsePathPHPEmitsMagicMethodClassification` |
| Trait adaptations (insteadof, as) | `php_language_trait_adaptation_test.go:TestDefaultEngineParsePathPHPEmitsTraitAdaptationMetadata` |
| Typed this-property chain inference (`$this->service->info()`) | `php_language_test.go:TestDefaultEngineParsePathPHPInfersTypedThisPropertyReceiverCalls` |
| Typed parameter inference (`run(Service $service)` → `$service.info()`) | `php_language_typed_parameter_test.go:TestDefaultEngineParsePathPHPInfersTypedParameterReceiverCalls` |
| `new` expression receiver chain inference (`new Service()->info()`) | `php_language_alias_test.go:TestDefaultEngineParsePathPHPInfersAliasedNewExpressionReceiverCalls` |
| Variable aliasing inference (`$logger = $service`; `$logger->info()`) | `php_language_alias_test.go:TestDefaultEngineParsePathPHPInfersAliasedNewExpressionReceiverCalls` |
| This-property alias inference (`$logger = $this->service`) | `php_language_alias_test.go:TestDefaultEngineParsePathPHPInfersAliasedThisPropertyReceiverCalls` |
| Property chain alias inference (`$this->container->logger` → `$logger->info()`) | `php_language_alias_test.go:TestDefaultEngineParsePathPHPInfersPropertyChainAliasReceiverCalls` |
| Method return type chaining (`$this->factory->createService()->info()`) | `php_language_method_chain_test.go:TestDefaultEngineParsePathPHPInfersMethodReturnCallChainReceiverCalls` |
| Method return + property dereference (`createService()->logger->info()`) | `php_language_method_chain_test.go:TestDefaultEngineParsePathPHPInfersMethodReturnPropertyDereferenceReceiverCalls` |
| Free function return type chaining (`createService()->info()`) | `php_language_function_chain_test.go:TestDefaultEngineParsePathPHPInfersDirectFreeFunctionReturnReceiverCalls` |
| Free function return call chain (`createFactory()->createService()->info()`) | `php_language_function_chain_test.go:TestDefaultEngineParsePathPHPInfersFreeFunctionReturnCallChainReceiverCalls` |
| Free function return + property chain (`createFactory()->logger`) | `php_language_property_chain_alias_test.go:TestDefaultEngineParsePathPHPInfersFreeFunctionReturnPropertyChainReceiverCalls` |
| Chained static factory (`Factory::instance()->createService()->info()`) | `php_language_alias_test.go:TestDefaultEngineParsePathPHPInfersChainedStaticFactoryReceiverCalls` |
| Import alias receiver inference (`use X as Y`; `new Y()`) | `php_language_alias_test.go:TestDefaultEngineParsePathPHPInfersImportedTypeAliasReceiverCalls` |
| Import alias static chain (`AppFactory::instance()->createService()->info()`) | `php_language_alias_test.go:TestDefaultEngineParsePathPHPInfersImportedStaticTypeAliasReceiverChains` |
| Self/static direct calls (`self::emit()`, `static::emit()`) | `php_language_self_static_direct_test.go:TestDefaultEngineParsePathPHPInfersDirectSelfAndStaticReceiverCalls` |
| Self/static in `new` chains (`new self()->createService()->info()`) | `php_language_self_static_new_test.go:TestDefaultEngineParsePathPHPInfersSelfAndStaticInstantiationReceiverCalls` |
| Parent static receiver chain (`parent::instance()->createService()->info()`) | `php_language_parent_static_test.go:TestDefaultEngineParsePathPHPInfersParentStaticReceiverCallChains` |
| Static property chains (`self::$service->info()`) | `php_language_static_property_receiver_test.go:TestDefaultEngineParsePathPHPInfersStaticPropertyReceiverChains` |
| Parent/static property chains (`parent::$service->info()`, `static::$service->info()`) | `php_language_static_property_receiver_test.go:TestDefaultEngineParsePathPHPInfersParentAndStaticPropertyReceiverChains` |
| Deep static property chains (`self::$factory->createService()->info()`) | `php_language_static_property_receiver_test.go:TestDefaultEngineParsePathPHPInfersDeepStaticPropertyReceiverChains` |
| Namespaced instantiation (`new Demo\Service()`, parenthesized new chain) | `php_language_namespaced_new_test.go:TestDefaultEngineParsePathPHPInfersNamespacedInstantiationReceiverCalls` |
| Parenthesized receiver normalization (`($expr)->method()`) | `php_language_parenthesized_receiver_test.go:TestDefaultEngineParsePathPHPInfersParenthesizedMethodReturnCallChainReceiverCalls` |
| Call context line metadata | `php_language_test.go:TestDefaultEngineParsePathPHPEmitsCallContextLineMetadata` |
| Multiline call arguments | `php_language_test.go:TestDefaultEngineParsePathPHPMultilineArgumentsAndContextLineMetadata` |
| All 10 dead-code root kinds | `php_dead_code_roots_test.go:TestDefaultEngineParsePathPHPEmitsDeadCodeRootKinds` |
| Dead-code roots with PSR-2 next-line braces | `php_dead_code_roots_test.go:TestDefaultEngineParsePathPHPKeepsRootKindsForNextLineTypeBraces` |
| Ambiguous syntax NOT rooted (commented hooks, non-Route attributes, non-magic `__` methods) | `php_dead_code_roots_test.go:TestDefaultEngineParsePathPHPDoesNotRootAmbiguousSyntax` |
| Inherited interface method roots | `php_dead_code_roots_test.go:TestDefaultEngineParsePathPHPRootsInheritedInterfaceMethods` |
| Fixture-based dead code expectation gate | `php_dead_code_roots_test.go:TestDefaultEngineParsePathPHPDeadCodeFixtureExpectedRoots` |
| Cyclomatic complexity (straight-line = 1, branches = 8) | `engine_cyclomatic_complexity_test.go` (php_straight_line, php_branches_and_boolean) |
| Comprehensive fixture gate (traits bucket, static calls, alias calls, cross-file) | `engine_long_tail_test.go:TestDefaultEngineParsePathPHPFixtures` |
| Self/static scoped call metadata (full_name, inferred_obj_type) | `engine_long_tail_test.go:TestDefaultEngineParsePathPHPResolvesSelfStaticReceiverMetadata` |

## Unverified / Claimed-but-Untested Constructs
Constructs present in source code but not covered by any test assertion.

| Construct | Source | Gap |
|---|---|---|
| Constructor property promotion (`property_promotion_parameter`) | `declarations.go:199,215-221` | The `php_comprehensive/classes.php` fixture uses `private float $radius` but no test asserts that the promoted parameter is emitted as a variable row with its type or recorded as a class property type. |
| Variadic parameters (`variadic_parameter`) | `declarations.go:199`, `support.go:183` | No test feeds `...$args` into a function and verifies parameter name capture. |
| Union types (`int\|string`) | `support.go:125`, `support.go:161`, `type_names.go:15-26` | No test uses a union-typed property, parameter, or return type and verifies the normalized `type` field. |
| Intersection types (`Countable&Iterator`) | `support.go:125`, `support.go:161` | No test. |
| Anonymous functions / closures (`function() {...}`) | `complexity.go:33` | Complexity set tracks them but no parse test creates an anonymous function and verifies function emission. |
| Arrow functions (`fn($x) => $x`) | `complexity.go:34` | Not tested. |
| PHP 8.1 enums (`enum_declaration`) | (not handled) | The parser has no `enum_declaration` case. Enums are silently ignored — they emit no class row, no method, and no variable. |
| PreScan | `parser.go:111-119`, `php_language.go:22-35` | No PHP-specific PreScan test; engine-level PreScan tests use other languages (JS/TS, Groovy, Kotlin). |
| `$this` exclusion from variable bucket | `variable_emit.go:19` | No test asserts `$this` is absent from the variables bucket. |
| Variable deduplication | `variable_emit.go:46-49` | No test creates two occurrences of the same `$name` in a file and verifies exactly one variable row appears. |
| Call deduplication | `calls.go:28-30` | No test creates two identical calls on the same line and verifies only one call row. |
| Namespace field in payload | `parser.go:97-99` | Most fixtures declare a namespace, but no test reads `payload["namespace"]` directly. |
| Empty namespace (no `namespace` declaration) | `parser.go:181-198` | No test asserts the namespace field is absent when no `namespace` is declared. |
| Trait `use` inside class excluded from import bucket | `imports.go:17-19` (phpNodeInsideType) | No test verifies that `use Trait;` inside a class body does NOT generate an import row. |
| Import `context` field | `imports.go:40` | No test checks the `context: [nil, nil]` field on import rows. |
| Function `decorators` field | `declarations.go:147` | No test checks the `decorators: []string{}` placeholder. |
| Call `args` for variadic/named argument syntax | `call_emit.go:200-218` | Named args are tested implicitly through multiline args; variadic args are not. |
| Return type for methods (declared in declared type) | `support.go:143-168` | Tested indirectly via method-return chaining tests but never asserted with `phpAssertStringFieldValue(t, item, "return_type", "X")` on a method. Free function return types are asserted. |

## Edge Cases Considered
Edge cases the tests actually cover.

| Edge case | Test reference |
|---|---|
| Nullsafe operator `?->` normalized to `->` in full_name and receiver inference | `php_language_test.go:TestDefaultEngineParsePathPHPEmitsNullsafeReceiverMetadata` |
| Chained nullsafe (`$session?->service?->info()`) still resolves type correctly | `php_language_test.go:TestDefaultEngineParsePathPHPEmitsNullsafeReceiverMetadata` |
| PSR-2 next-line brace style for interfaces/traits/classes does not break dead-code roots | `php_dead_code_roots_test.go:TestDefaultEngineParsePathPHPKeepsRootKindsForNextLineTypeBraces` |
| Commented-out `add_action` and `Route::get` calls do not produce dead-code roots | `php_dead_code_roots_test.go:TestDefaultEngineParsePathPHPDoesNotRootAmbiguousSyntax` |
| Non-Symfony `#[MyRoute]` attribute does not produce `php.symfony_route_attribute` | `php_dead_code_roots_test.go:TestDefaultEngineParsePathPHPDoesNotRootAmbiguousSyntax` |
| Double-underscore methods that are not magic (`__legacyHelper`) are not classified as magic or rooted | `php_dead_code_roots_test.go:TestDefaultEngineParsePathPHPDoesNotRootAmbiguousSyntax` |
| Inherited interface methods recognized through transitive interface chains (`ChildContract extends ParentContract`) | `php_dead_code_roots_test.go:TestDefaultEngineParsePathPHPRootsInheritedInterfaceMethods` |
| Nested interface chains do not cause infinite loops (cycle detection in `phpInterfaceHasMethod`) | `php_dead_code_roots_test.go:TestDefaultEngineParsePathPHPRootsInheritedInterfaceMethods` |
| Unused function has no dead_code_root_kinds | `php_dead_code_roots_test.go:TestDefaultEngineParsePathPHPEmitsDeadCodeRootKinds` |
| Private helper method in Controller has no dead_code_root_kinds | `php_dead_code_roots_test.go:TestDefaultEngineParsePathPHPEmitsDeadCodeRootKinds` |
| Controller method without route backing is not rooted as controller action | `php_dead_code_roots_test.go:TestDefaultEngineParsePathPHPEmitsDeadCodeRootKinds` (supportOnly) |
| `__invoke` is rooted as `php.magic_method` | `php_dead_code_roots_test.go` (both test 1 and test 5) |
| Parenthesized receiver `($expr)->method()` resolves type through chain | `php_language_parenthesized_receiver_test.go:TestDefaultEngineParsePathPHPInfersParenthesizedMethodReturnCallChainReceiverCalls` |
| Self/static resolution in `new self()` / `new static()` chains | `php_language_self_static_new_test.go:TestDefaultEngineParsePathPHPInfersSelfAndStaticInstantiationReceiverCalls` |
| Parent scope resolution in scoped call chains (`parent::instance()->createService()->info()`) | `php_language_parent_static_test.go:TestDefaultEngineParsePathPHPInfersParentStaticReceiverCallChains` |
| Namespaced class resolution in `new` (`new Demo\Service()`) | `php_language_namespaced_new_test.go:TestDefaultEngineParsePathPHPInfersNamespacedInstantiationReceiverCalls` |
| Parenthesized new expression on receiver chain (`(new Demo\Service())->run()`) | `php_language_namespaced_new_test.go:TestDefaultEngineParsePathPHPInfersNamespacedInstantiationReceiverCalls` |
| Import alias resolving to last segment in type inference (`use Demo\Library\Config as AppConfig` → type = `Config`) | `php_language_alias_test.go:TestDefaultEngineParsePathPHPInfersImportedTypeAliasReceiverCalls` |
| Import alias resolving in static receiver context (`AppFactory::instance()`) | `php_language_alias_test.go:TestDefaultEngineParsePathPHPInfersImportedStaticTypeAliasReceiverChains` |
| Property type from nullable declaration (`?Service`) → normalized to `Service` | `php_language_test.go:TestDefaultEngineParsePathPHPEmitsPropertyTypeInferenceFromDeclaration` |
| Multiline call arguments preserved as raw source text with indentation | `php_language_test.go:TestDefaultEngineParsePathPHPMultilineArgumentsAndContextLineMetadata` |
| Call context tuple (name, kind, line) for method-declaration context | `php_language_test.go:TestDefaultEngineParsePathPHPEmitsCallContextLineMetadata` |
| Call context line metadata tracks the line of the enclosing function/method name | Many tests via `assertCallContextTuple` |
| Grouped `use` with alias (`Logger\Stream as StreamLogger`) | `php_language_test.go:TestDefaultEngineParsePathPHPEmitsGroupedUseImportMetadata` |
| Grouped `use function` / `use const` with mixed aliases | `php_language_test.go:TestDefaultEngineParsePathPHPEmitsGroupedUseFunctionAndConstImportKinds` |
| Trait `insteadof` and `as` adaptation text normalized (whitespace collapsed, semicolon stripped) | `php_language_trait_adaptation_test.go:TestDefaultEngineParsePathPHPEmitsTraitAdaptationMetadata` |
| Anonymous class extends a named class and bases are captured | `php_language_anonymous_test.go:TestDefaultEngineParsePathPHPEmitsAnonymousClassMetadata` |
| Anonymous class used as variable type for receiver inference | `php_language_anonymous_test.go:TestDefaultEngineParsePathPHPEmitsAnonymousClassMetadata` |
| Assignment-based type inference (RHS type recorded and used for later uses of same variable) | `php_language_alias_test.go` (multiple tests: `$logger = $service`, `$logger = $this->service`, `$logger = createFactory()->logger`) |
| Three-level dynamic chain (`$this->factory->createService()->logger->info()`) | `php_language_method_chain_test.go:TestDefaultEngineParsePathPHPInfersMethodReturnPropertyDereferenceReceiverCalls` |

## Edge Cases NOT Considered
Edge cases with no test coverage.

| Gap | Why it matters |
|---|---|
| **Union types** (`string\|int`, `?string`) on properties, parameters, and return types | Used in modern PHP; type_names.go has splitting logic that may be untested end-to-end. |
| **Intersection types** (`Countable&Iterator`) | PHP 8.1+ feature; the parser handles `intersection_type` node kind but it is untested. |
| **Constructor property promotion** (`__construct(public Service $svc)`) | Widely used in PHP 8+; fixtures include it but no test asserts variable/property extraction. |
| **Variadic parameters** (`function foo(string ...$args)`) | Parameter name extraction handles `variadic_parameter` but no test confirms. |
| **PHP 8.1 enums** | The parser has zero handling for `enum_declaration`; enum methods are invisible. Should at minimum document this as a known gap. |
| **Anonymous functions / closures** (`$fn = function() { ... };`) | Complexity tracking mentions them but no parse test emits a closure and asserts its row. |
| **Arrow functions** (`fn($x) => $x`) | Unclear if arrow functions produce any evidence at all; no test. |
| **Named arguments** (`foo(name: "value")`) | The parser captures raw args; named syntax may break arg text extraction. No focused test. |
| **Match expressions** | Complexity set has `match_conditional_expression` and `match_default_expression` but no parse test. |
| **PreScan correctness** | No PHP-specific PreScan test verifies names match between Parse and PreScan. |
| **`$this` filtering from variables bucket** | No test asserts `$this` is excluded from the variables list. |
| **Variable deduplication** | No test verifies that two `$foo` mentions produce exactly one row. |
| **Call deduplication** | No test verifies that `foo(); foo();` on the same line emits one call row. |
| **Namespace extraction from payload** | Most fixtures declare a namespace but no test reads `payload["namespace"]`. |
| **Multiple namespace definitions in one file** | PHP allows multiple `namespace A { } namespace B { }` — the parser takes only the first; not tested. |
| **Bracketless namespace** (`namespace Foo;`) | Common syntax; fixtures use it implicitly but no test asserts namespace extraction explicitly. |
| **PHP 8.0 attributes on classes/traits (not just methods)** | Only method-level attribute observation is tested (via `#[Route]`). |
| **Attributes with namespace prefix** (`#[Symfony\Component\Routing\Annotation\Route]`) | Only bare `Route` and `Symfony\Component\Routing\Attribute\Route` are listed in `phpIsSymfonyRouteAttribute`; the annotation variant is listed but not tested. |
| **Circular inheritance** | If class A extends B and B extends A (parseable in separate files), could infinite-loop in property-type walking — cycle detection exists but is untested for the property-type path. |
| **Trait use without adaptations inside class** | The parser excludes `use Trait;` inside class body from imports (phpNodeInsideType). No test verifies this exclusion. |
| **Deeply nested call arguments** (e.g., `foo(bar(baz()))`) | Only single-level call args are tested. |
| **Control-flow context** (calls inside if/else/loop/switch) | All tests use straight-line code. No test verifies correct extraction under branches. |
| **IndexSource mode** (source content in function rows) | Only `TestDefaultEngineParsePathPHPEmitsFunctionParametersSourceAndContext` asserts source; dead-code tests pass `IndexSource: true` but don't assert source content. |

## Verdict
**Deep** — The PHP parser has extensive, targeted test coverage across 15 dedicated test files plus engine-level complexity and comprehensive fixture tests. Every claim in `doc.go` is exercised. Receiver inference is tested across 30+ distinct scenarios. Dead-code root classification has its own 5-test suite covering all 10 root kinds, PSR-2 style, false-positive prevention, and inherited interface chains. The gaps are real but narrow: PHP 8.1 enums are a notable missing feature, and several edge cases (union/intersection types, property promotion, closures, deduplication) lack explicit assertions despite being part of the code path.

## Recommended Actions
1. **Add PHP 8.1 enum support** (or document as a known limitation): `enum_declaration` is not handled anywhere in the parser. Add a case to emit enum rows into the classes bucket, capture enum cases, and handle enum method bodies.
2. **Add property promotion test**: The `php_comprehensive/classes.php` fixture already uses `private float $radius` in constructor. Add an assertion in `TestDefaultEngineParsePathPHPFixtures` that verifies promoted parameters emit variable rows with correct types.
3. **Add union/intersection type tests**: Create a test with `string|int` property, `Countable&Iterator` parameter, and `string|false` return type; assert the normalized type strings in the payload.
4. **Add deduplication tests**: One test for variable dedup (same `$var` referenced twice → one row), one for call dedup (same method call on same line → one row).
5. **Add `$this` exclusion test**: Assert that `$this` does not appear in the variables bucket.
6. **Add namespace field assertion**: Add a test that reads `payload["namespace"]` directly to lock in the extraction contract.
7. **Add PreScan PHP test**: Create `TestDefaultEnginePreScanPathsPHP` to verify PreScan returns function/class/trait/interface names consistent with Parse and handles the empty-file case correctly.
8. **Add closure/anonymous function test**: Parse a file containing `$fn = function(int $x): int { return $x; };` and verify the anonymous function appears in the functions bucket.
9. **Document PHP 8.0+ feature support matrix** in the README with clear yes/no/partial status for enums, match, named args, attributes, union types, intersection types, property promotion, nullsafe, constructor promotion, and readonly properties.
