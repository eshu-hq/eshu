# Swift Parser Audit

## Overview
The Swift parser (`go/internal/parser/swift/`) uses a tree-sitter AST walk to extract imports, nominal types (classes, structs, enums, protocols, actors), functions, properties, calls, and 14 dead-code root kinds. It replaced a prior line-scan regex approach in #3589. Extension members now carry correct `class_context`. Only genuine `call_expression` nodes produce `function_calls` rows, fixing false positives from enum case declarations, `mutating`/`override` declaration lines, `private(set)` modifiers, and string interpolation. Vapor route handler detection remains a documented permanent exception (reads `use:` argument labels without symbol rows). The package holds 6 external `package swift_test` files (and one helper file) that drive the public `parser.DefaultEngine().ParsePath` contract from inside the package directory: `engine_swift_ast_migration_test.go`, `engine_swift_extension_test.go`, `engine_swift_semantics_test.go`, `engine_swift_vapor_routes_test.go`, `swift_dead_code_roots_test.go`, and `swift_vapor_golden_fixture_test.go`. The parent `go/internal/parser` directory keeps only cross-language Swift coverage: the Kotlin+Swift comprehensive symbol-extraction gate and the long-tail and cyclomatic-complexity fixture sweeps.

## Claimed Constructs
| Construct | Source Reference |
|---|---|
| `imports` | `ast_extract.go:110-128` (`handleImport`) |
| `classes` (actor, class) | `ast_extract.go:133-153` (`handleTypeDeclaration`), `ast_extract.go:269-280` (`swiftTypeBucketKind`) |
| `structs` | `ast_extract.go:133-153`, `ast_extract.go:274` |
| `enums` | `ast_extract.go:133-153`, `ast_extract.go:276` |
| `protocols` | `ast_extract.go:167-189` (`handleProtocolDeclaration`) |
| `functions` (including `init`) | `ast_extract.go:225-264` (`handleFunction`), lines 80-85 dispatch |
| `variables` (properties) | `ast_extract.go:191-223` (`handleProperty`) |
| `function_calls` (plain + receiver) | `ast_calls.go:31-61` (`handleCall`), `ast_calls.go:64-79` (`swiftCallTarget`) |
| Call arguments | `ast_calls.go:104-128` (`swiftCallArguments`) |
| `inferred_obj_type` | `ast_calls.go:59` |
| `bases` (inheritance/conformance) | `ast_nodes.go:81-102` (`swiftInheritanceBases`) |
| `args` (parameter names) | `ast_nodes.go:107-123` (`swiftParameterNames`) |
| `class_context` | `ast_extract.go:250-252`, extension scope on line 160-163 |
| Extension type name resolution | `tree_sitter_syntax.go:142-152` (`swiftExtensionTypeName`) |
| `dead_code_root_kinds` | `helpers.go:14-87` |
| `cyclomatic_complexity` | `complexity.go:41-43` |
| PreScan names | `language.go:48-55` |
| `source` (IndexSource) | `ast_extract.go:253-255` |
| `decorators` | `ast_extract.go:247` (empty slice sentinel) |

Dead-code root kinds claimed: `swift.protocol_type`, `swift.main_type`, `swift.swiftui_app_type`, `swift.ui_application_delegate_type`, `swift.swiftui_body`, `swift.main_function`, `swift.constructor`, `swift.protocol_method`, `swift.override_method`, `swift.protocol_implementation_method`, `swift.ui_application_delegate_method`, `swift.vapor_route_handler`, `swift.xctest_method`, `swift.swift_testing_method`.

## Verified-by-Test Constructs
| Construct | Test Reference |
|---|---|
| `imports` (name, full_import_name, is_dependency) | `swift/engine_swift_semantics_test.go:56-72` (`TestDefaultEngineParsePathSwiftEmitsImportAndCallMetadata`) |
| `classes`, `structs`, `enums`, `protocols` with `bases` | `swift/engine_swift_semantics_test.go:14-34` (`TestDefaultEngineParsePathSwiftEmitsBasesAndFunctionArgs`), lines 131-205 (multiline) |
| `functions` with `args` | `swift/engine_swift_semantics_test.go:31-32` |
| `variables` with `type`, `context`, `class_context` | `swift/engine_swift_semantics_test.go:36-54` (`TestDefaultEngineParsePathSwiftEmitsVariableContextAndTypeMetadata`) |
| `function_calls` (name, full_name) | `swift/engine_swift_semantics_test.go:56-72` (`assertSwiftCallMetadata`), `swift/engine_swift_ast_migration_test.go:90-91` |
| `inferred_obj_type` (receiver call type) | `swift/engine_swift_semantics_test.go:83-129` (`TestDefaultEngineParsePathSwiftInfersReceiverCallTypesAndEmitsProtocols`) |
| Extension method `class_context` | `swift/engine_swift_extension_test.go:13-63` |
| Call false-positive removal (enum case, mutating, override, private(set), bark) | `swift/engine_swift_ast_migration_test.go:15-92` |
| Override detection from `member_modifier` (not body text) | `swift/engine_swift_ast_migration_test.go:94-151` |
| Full body `source` span (IndexSource) | `swift/engine_swift_ast_migration_test.go:153-191` |
| `line_number`, `end_line` | `swift/engine_swift_semantics_test.go:192-193` |
| PreScan names (functions, classes, structs, enums, protocols) | `swift/engine_swift_semantics_test.go:196-204` |
| All 14 dead-code root kinds | `swift/swift_dead_code_roots_test.go:14-107` (`TestDefaultEngineParsePathSwiftEmitsDeadCodeRootKinds`) |
| Negative: helper/private NOT rooted | `swift/swift_dead_code_roots_test.go:109-122` |
| Negative: init in protocol NOT `swift.constructor` | `swift/swift_dead_code_roots_test.go:95` |
| Negative: `@available`/`@Test` NOT in `function_calls` | `swift/swift_dead_code_roots_test.go:105-106` |
| `cyclomatic_complexity` | `engine_cyclomatic_complexity_test.go:230-238` (table-driven, swift-specific fixtures) |

## Unverified / Claimed-but-Untested Constructs
- **`decorators`**: Always emitted as `[]string{}` (empty sentinel). No test asserts presence or content of the `decorators` field for Swift functions.

## Edge Cases Considered
| Edge Case | Test Reference |
|---|---|
| Enum case declarations not treated as calls | `swift/engine_swift_ast_migration_test.go:76-79` |
| `mutating func` / `override func` declaration lines not treated as calls | `swift/engine_swift_ast_migration_test.go:80-82` |
| `private(set)` modifier not treated as a call | `swift/engine_swift_ast_migration_test.go:83` |
| `override` keyword in comment/string literal does not root | `swift/engine_swift_ast_migration_test.go:94-151` |
| Extension methods carry class_context for protocol and struct extensions | `swift/engine_swift_extension_test.go:58-62` |
| Multiline declarations (class, struct, protocol across lines) | `swift/engine_swift_semantics_test.go:131-205` |
| `init` in protocol scope NOT `swift.constructor` | `swift/swift_dead_code_roots_test.go:94-95` |
| `super`/`self` receiver calls keep their receiver text | `ast_calls.go:92` (`super_expression`, `self_expression` as receiver) |
| Wildcard `_` parameter labels dropped | `ast_nodes.go:116` |
| Chained navigation expression calls recorded | `ast_calls.go:86-102` |

## Edge Cases NOT Considered
No test covers: nested types, generic type parameters, closures/lambdas, deinitializers, subscripts, associated types, `where` clauses, result builders, property wrappers, access control beyond overrides, lazy properties, computed vs stored properties, `didSet`/`willSet`, `async`/`throws` functions, operator overloads, precedence groups, macros, package declarations, empty source files, invalid Swift syntax, or very large files.

## Verdict
**Deep** — All 14 dead-code root kinds are individually asserted with positive and negative cases. Every payload bucket (imports, classes, structs, enums, protocols, functions, variables, calls) is verified for shape and content. False-positive regression for the regex→AST migration is explicitly tested. Extension class_context, multiline scope, receiver type inference, and override detection from AST modifiers (not body text) are all covered. The only gap is the `decorators` field (always empty sentinel), which is minor.

## Recommended Actions
1. Add a test asserting `decorators` field is present and empty for Swift functions (currently just set to `[]string{}`).
2. Add a test for nested type declarations (class inside class) to verify inner type scope and member attribution.
3. Add a test for Swift closure/lambda expressions to verify they don't break scope tracking.
4. Add a test for `subscript` declarations to verify they're not misclassified as functions or calls.
5. Add a fixture test with `async throws` functions to verify parameter and call extraction under async syntax.
