# Dart Parser Audit

## Overview
The Dart parser (`go/internal/parser/dart/`) uses tree-sitter syntax for import/export, class-style declarations, function rows, variables, and Dart/Flutter dead-code root metadata, while keeping call evidence in a legacy line-scanning approach in `calls.go`. It has 1 subdirectory test file (`parser_test.go`) with 8 test functions, and 0 parent-level Dart-named test files. Dart test coverage at the parent parser level is limited to a fixture corpus test (`engine_long_tail_test.go:246`) and cyclomatic complexity tests (`engine_cyclomatic_complexity_test.go:246-254`).

## Claimed Constructs
From `doc.go:6-13`, `README.md:6-8`, and function docstrings:

| Construct | Source reference |
|---|---|
| Imports (`library_import`) | `syntax_index.go:74-81`, `parser.go:42-52` |
| Exports (`library_export`) | `syntax_index.go:82-89`, `parser.go:42-52` |
| Classes / mixins / enums / extensions | `syntax_index.go:90-93`, `parser.go:53-62` |
| Functions (including methods) | `syntax_index.go:95-98`, `parser.go:63-88` |
| Constructors | `syntax_index.go:100-106`, `README.md:42-44` |
| Variables | `syntax_index.go:114-117`, `parser.go:89-100` |
| Function calls (legacy line-scan) | `calls.go:14-53` (`appendDartCalls`), `README.md:7-8` |
| Cyclomatic complexity | `complexity.go:43` (`dartCyclomaticComplexity`) |
| Dead-code roots: `dart.main_function` | `parser.go:143-144` |
| Dead-code roots: `dart.public_library_api` | `parser.go:146-148`, `parser.go:163-165` |
| Dead-code roots: `dart.override_method` | `parser.go:151-152` |
| Dead-code roots: `dart.flutter_widget_build` | `parser.go:154-156` |
| Dead-code roots: `dart.flutter_create_state` | `parser.go:157-159` |
| Dead-code roots: `dart.constructor` | `parser.go:160-162` |
| Decorator extraction (annotations) | `syntax_index.go:316-329` (`dartDecoratorsBeforeLine`), `parser.go:75-76` |
| Multiline method signatures | `syntax_index.go:249-257` (`dartFunctionEndLine`) |
| Import type labeling (`import_type`) | `parser.go:48-50`, `README.md:47-49` |
| PreScan (functions, classes) | `parser.go:192-200` |

## Verified-by-Test Constructs
Tests in `dart/parser_test.go` verify:

| Construct | Test reference |
|---|---|
| Imports with `import_type=import` | `parser_test.go:29-31` (`TestParseCapturesDartBuckets`) |
| Exports with `import_type=export` | `parser_test.go:33-35` |
| Deduplicated imports (no double-counting wrapper+child) | `parser_test.go:37` |
| Classes | `parser_test.go:47` (`TestPreScanReturnsDartDeclarations`) |
| Functions | `parser_test.go:47` |
| PreScan returns function+class names | `parser_test.go:47` |
| `dart.main_function` | `parser_test.go:113` (`TestParseMarksDartDeadCodeRoots`) |
| `dart.constructor` | `parser_test.go:114,115` |
| `dart.flutter_create_state` | `parser_test.go:116` |
| `dart.flutter_widget_build` | `parser_test.go:118` |
| `dart.override_method` | `parser_test.go:117,119` |
| `dart.public_library_api` (classes) | `parser_test.go:120` |
| `dart.public_library_api` (functions) | `parser_test.go:121` |
| `dart.main_function` not on class named "main" | `parser_test.go:124` |
| `dart.flutter_widget_build` via `State<Widget>` extends | `parser_test.go:126` |
| Decorators do not leak from class to methods | `parser_test.go:136-154` (`TestParseDoesNotLeakDartAnnotationsFromFields`) |
| `dart.override_method` on methods with `@override` | `parser_test.go:154` (negation assertion) |
| Constructor from `Demo.named()` syntax | `parser_test.go:160` (`TestParseCapturesConstructorFromNamedConstructor`) |
| Multiline method signature spans | `parser_test.go:186` (`TestParseCapturesMultilineDartMethodSignature`) |
| Comment-only lines do not produce call rows | `parser_test.go:216` (`TestParseSkipsDartCommentOnlyCalls`) |
| Cyclomatic complexity (straight-line, branches, boolean) | `engine_cyclomatic_complexity_test.go:246-254` |

Parent-level fixture corpus test (not subdirectory):
| Dart fixture corpus | `engine_long_tail_test.go:246` (`TestDefaultEngineParsePathDartFixtures`) |
| Runtime grammar loading | `runtime_test.go:24` (`TestRuntimeParserLoadsDartGrammar`) |

## Unverified / Claimed-but-Untested Constructs
- **Extension declarations** — claimed in `syntax_index.go:90` (`extension_declaration`, `extension_type_declaration`) but no fixture exercises extension bodies or extension member extraction.
- **Mixin declarations** — claimed in `syntax_index.go:90` (`mixin_declaration`), no test.
- **Enum declarations** — claimed in `syntax_index.go:90` (`enum_declaration`), no test.
- **Factory constructors** — claimed in `syntax_index.go:100` (`factory_constructor_signature`, `redirecting_factory_constructor_signature`), no test.
- **Getters and setters** — `getter_signature` / `setter_signature` referenced in `syntax_index.go:176`, not tested.
- **Variables** — `parser.go:89-100` emits `variables` bucket but no test asserts variable names or counts.
- **Call extraction** — `calls.go:14-53` (`appendDartCalls`) performs line-scanning call extraction with comment/string-literal skipping. No test asserts specific call rows or deduplication. The `TestParseSkipsDartCommentOnlyCalls` test only checks that comment-only lines don't produce calls, not that real calls are extracted.
- **`library_import` vs `library_export` classification** — only tested in parent `engine_long_tail_test.go` via the Dart fixture corpus, not in `dart/parser_test.go`.
- **`lib/src/` path exclusion** — `parser.go:188` (`isPublicLibraryPath`) claims to exclude `lib/src/` paths from `dart.public_library_api`. No test verifies a function in `lib/src/` does NOT get a public API root.
- **`Static_final_declaration` variables** — claimed in `syntax_index.go:114`, not tested.
- **Call keyword filtering** — `dartCallKeyword` in `calls.go:144-151` (assert, catch, for, if, switch, while), not tested.

## Edge Cases Considered
- Imports not double-counted (import_or_export wrapper vs concrete child): `parser_test.go:37`
- Decorators on class do not leak to methods: `parser_test.go:136-154`
- Class named "main" is not `dart.main_function`: `parser_test.go:124`
- `State<Widget>` extension match for `build` method: `parser_test.go:126`
- Multiline signatures with end_line tracking: `parser_test.go:186`
- Comment line-call suppression: `parser_test.go:216`
- Dart async fixture (`dart:async` import): `engine_long_tail_test.go:262`
- Triple-quoted strings in call scanner: `calls.go:94-97`
- Brace escape group expansion in imports: module reference (`Foo.Bar`) not treated as call name: `ast_nodes.go:137-139`

## Edge Cases NOT Considered
- Empty Dart source file
- Dart file with only comments and whitespace
- Deeply nested class hierarchies
- Unicode identifiers in Dart source
- Triple-quoted string literals containing `(` characters (could produce false call rows)
- Inline string interpolation with calls (`"${foo.bar()}"`)
- Late variables (`late` keyword)
- Extension methods with `on` clause
- Sealed class hierarchies
- Mixin applications (`with` clause)
- Record types
- Pattern matching (Dart 3.0)
- Generic type arguments on methods
- Async generator functions (`async*`, `sync*`)
- Call extraction from `lib/src/` paths

## Verdict
**shallow**

The Dart parser has a single subdirectory test file with 8 test functions focused primarily on dead-code root kinds and bucket shape. It has 0 parent-level Dart-named test files. Several claimed constructs (enums, mixins, extension declarations, factory constructors, getters/setters, variables, call extraction) have no dedicated assertions. The parent-level test `engine_long_tail_test.go:246` exercises a fixture corpus but only checks for `dart:async` import presence, not comprehensive symbol coverage.

## Recommended Actions
1. Add test coverage for enums, mixins, and extension declarations with a corpus fixture.
2. Add focused tests for factory constructors and getter/setter extraction.
3. Add assertions for the `variables` bucket output.
4. Add tests verifying `lib/src/` path exclusion from `dart.public_library_api`.
5. Add tests for call extraction (real function calls, not just comment suppression).
6. Add a dedicated call deduplication test.
7. Add an empty file / whitespace-only parse test.
8. Add tests for edge cases: string interpolation with calls, nested triple-quoted strings, late variables, sealed classes.
9. Create parent-level test files (e.g., `dart_dead_code_roots_test.go`) that follow the pattern of `cpp_dead_code_roots_test.go` to test the parser through the Engine dispatch path.
