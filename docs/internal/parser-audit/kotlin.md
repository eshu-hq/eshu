# Kotlin Parser Audit

## Overview

The Kotlin parser (`go/internal/parser/kotlin/`) is a pure tree-sitter AST walker
that replaces a prior hybrid regex/line-scan extraction (issue #3533). It emits
five payload buckets — `functions`, `classes`, `interfaces`, `variables`,
`imports`, `function_calls` — plus cyclomatic complexity, dead-code root
classification, smart-cast flow, scope-function transparency, receiver/type
inference, and package-bounded sibling return-type lookups. The 14 source files
(no tests in-package) are exercised by 16 parent-level test files (~54 test
functions) in `go/internal/parser/`. Coverage is broad and intentional.

## Claimed Constructs

All claims below are drawn from source comments, `doc.go`, `README.md`, and the
actual AST dispatch in `ast_walk.go:walkNode`.

### Payload buckets

| Bucket | Source |
|---|---|
| `functions` | `ast_functions.go:28` (function_declaration), `ast_functions.go:79` (secondary_constructor) |
| `classes` | `ast_declarations.go:286` (class_declaration, object_declaration, companion_object) |
| `interfaces` | `ast_declarations.go:286` (class_declaration with `interface` keyword) |
| `variables` | `ast_variables.go:39` (property_declaration) |
| `imports` | `ast_declarations.go:326` (import) |
| `function_calls` | `ast_calls.go:79` (call_expression), `ast_calls.go:26` (infix_expression) |

### Function row fields

| Field | Source |
|---|---|
| `name` | `ast_functions.go:29` |
| `line_number`, `end_line` | `ast_functions.go:30-31` |
| `lang` | `ast_functions.go:32` |
| `decorators` | `ast_functions.go:33` (always empty slice) |
| `cyclomatic_complexity` | `ast_functions.go:34`, `complexity.go:42` |
| `suspend` | `ast_functions.go:36-38` |
| `extension_receiver` | `ast_functions.go:41-46` |
| `class_context` | `ast_functions.go:47-49` |
| `dead_code_root_kinds` | `ast_functions.go:56-67`, `dead_code_roots.go` |
| `source` (IndexSource) | `ast_functions.go:69-71` |
| `constructor_kind` | `ast_functions.go:83` (secondary_constructor only) |

### Class/interface row fields

| Field | Source |
|---|---|
| `name`, `line_number`, `end_line`, `lang` | `ast_declarations.go:286-291` |
| `dead_code_root_kinds` | `ast_declarations.go:292-294`, `dead_code_roots.go:27` |

### Import row fields

| Field | Source |
|---|---|
| `name`, `source`, `alias`, `full_import_name`, `import_type`, `line_number`, `lang` | `ast_declarations.go:326-335` |

### Variable row fields

| Field | Source |
|---|---|
| `name`, `line_number`, `end_line`, `lang` | `ast_variables.go:39-45` |

### Call row fields

| Field | Source |
|---|---|
| `name`, `full_name`, `line_number`, `lang` | `ast_calls.go:55-60`, `ast_calls.go:125-132` |
| `inferred_obj_type` | `ast_calls.go:60`, `ast_calls.go:210` |
| `class_context` | `ast_calls.go:62-64`, `ast_calls.go:204-206` |
| `call_kind` | `ast_calls.go:212-214` |

### Dead-code root kinds (19)

| Root kind | Source |
|---|---|
| `kotlin.interface_type` | `dead_code_roots.go:30` |
| `kotlin.spring_component_class` | `dead_code_roots.go:32-36` |
| `kotlin.spring_configuration_properties_class` | `dead_code_roots.go:37-39` |
| `kotlin.main_function` | `dead_code_roots.go:58-60` |
| `kotlin.interface_method` | `dead_code_roots.go:61-63` |
| `kotlin.override_method` | `dead_code_roots.go:64-66` |
| `kotlin.interface_implementation_method` | `dead_code_roots.go:67-69` |
| `kotlin.gradle_plugin_apply` | `dead_code_roots.go:70-72` |
| `kotlin.gradle_task_action` | `dead_code_roots.go:73-75` |
| `kotlin.gradle_task_property` | `dead_code_roots.go:76-79` |
| `kotlin.gradle_task_setter` | `dead_code_roots.go:81-83` |
| `kotlin.spring_request_mapping_method` | `dead_code_roots.go:84-87` |
| `kotlin.spring_bean_method` | `dead_code_roots.go:88-90` |
| `kotlin.spring_event_listener_method` | `dead_code_roots.go:91-93` |
| `kotlin.spring_scheduled_method` | `dead_code_roots.go:94-96` |
| `kotlin.lifecycle_callback_method` | `dead_code_roots.go:97-99` |
| `kotlin.junit_test_method` | `dead_code_roots.go:100-103` |
| `kotlin.junit_lifecycle_method` | `dead_code_roots.go:104-106` |
| `kotlin.constructor` | `dead_code_roots.go:112-114` |

### Extraction logic

| Construct | Source |
|---|---|
| Bare function calls (same-scope, top-level, imported) | `ast_calls.go:93-122` |
| Constructor calls (known type + type-shaped name) | `ast_calls.go:107-110` |
| Navigation calls (receiver.method) | `ast_calls.go:172-217` |
| Infix calls (receiver name arg) | `ast_calls.go:26-66` |
| Chain-receiver dedup (callIsChainReceiver) | `ast_calls.go:134-163` |
| Scope-function suppression (also/apply) | `ast_calls.go:165-170`, `scope_function_helpers.go` |
| Smart-cast: `if (x is T)` | `ast_variables.go:174-202`, `ast_variables.go:227-239` |
| Smart-cast: `when (subject) { is T -> }` | `ast_variables.go:206-225`, `ast_variables.go:241-271` |
| Receiver type inference | `receiver_inference.go` |
| Type algebra (canonical, base, generic resolution) | `type_reference.go` |
| Sibling return-type collection | `repository_returns.go` |
| Package-aware return-type lookups | `repository_returns.go:136-158`, `repository_returns.go:181-211` |
| Lazy delegated property inference | `ast_variables.go:89-97`, `ast_variables.go:130-144` |
| Primary constructor property types | `ast_declarations.go:99-130` |
| Import alias detection | `ast_declarations.go:340-371` |
| Cyclomatic complexity (McCabe) | `complexity.go` |
| Pre-scan name extraction | `prescan.go` |

## Verified-by-Test Constructs

Tests live in `go/internal/parser/`. No test files exist in
`go/internal/parser/kotlin/`. The parent engine tests use `DefaultEngine()` →
`ParsePath()` with the Kotlin registered definition, exercising the full AST
walk path.

### Fixture corpus walk

| Test | File |
|---|---|
| `TestDefaultEngineParsePathKotlinComprehensiveFixturesParseCleanly` | `engine_kotlin_ast_test.go:18` |

### Functions bucket

| Test | File |
|---|---|
| Function with `class_context` (method on class) | `engine_kotlin_treesitter_test.go:49-52` |
| Function `suspend` flag (true/false) | `engine_kotlin_suspend_test.go:11` |
| Extension function `class_context` = Receiver | `engine_kotlin_call_metadata_test.go:120-122` |
| Function `line_number`, `end_line` | `engine_kotlin_treesitter_test.go:51-52` |
| Secondary constructor `constructor_kind` and `class_context` | `kotlin_dead_code_roots_test.go:101` |
| Interface method `class_context` = Interface name | `engine_kotlin_interface_test.go:49` |

### Classes bucket

| Test | File |
|---|---|
| Class declarations (data, sealed, abstract, concrete) | `engine_kotlin_swift_symbol_gate_test.go:28-32` |
| Object declarations | `engine_kotlin_call_metadata_test.go:326-360` |
| Companion objects | `engine_kotlin_call_metadata_test.go:362-413` |
| Nested class scoping (outer vs inner property isolation) | `engine_kotlin_treesitter_test.go:59-99` |
| Multiline class declarations | `engine_kotlin_treesitter_test.go:11-57` |

### Interfaces bucket

| Test | File |
|---|---|
| Interface declarations | `engine_kotlin_swift_symbol_gate_test.go:42-43` |
| Interface with `class_context` on methods | `engine_kotlin_interface_test.go:49` |

### Variables bucket

| Test | File |
|---|---|
| Property declarations (implied by type inference tests) | `engine_kotlin_call_metadata_test.go:58-122` |

### Imports bucket

| Test | File |
|---|---|
| Import rows with alias and import_type | `engine_kotlin_ast_test.go:158-163` |

### Function calls bucket — bare and constructor

| Test | File |
|---|---|
| Bare calls (same-scope, top-level) | `engine_kotlin_bare_calls_test.go:17` |
| Imported bare calls (top-level function from import) | `engine_kotlin_imported_bare_call_test.go:17` |
| Constructor calls (local type) | `engine_kotlin_constructor_calls_test.go:11` |
| Constructor calls (imported type, no alias) | `engine_kotlin_ast_test.go:128` |

### Function calls bucket — navigation and receivers

| Test | File |
|---|---|
| `this.receiver` carries `class_context` | `engine_kotlin_call_metadata_test.go:11` |
| Local variable receiver type inference | `engine_kotlin_call_metadata_test.go:58` |
| Cast receiver type (`any as Service` → `service.info`) | `engine_kotlin_call_metadata_test.go:124` |
| Direct cast receiver (`(any as Service).info`) | `engine_kotlin_call_metadata_test.go:171` |
| Infix call receiver type | `engine_kotlin_call_metadata_test.go:217` |
| Typed property alias chains | `engine_kotlin_call_metadata_test.go:265` |
| Object receiver types | `engine_kotlin_call_metadata_test.go:316` |
| Companion object receiver types | `engine_kotlin_call_metadata_test.go:362` |
| Generic nullable receiver types | `engine_kotlin_call_metadata_test.go:415` |
| Typed property chain calls | `engine_kotlin_call_metadata_test.go:476` |
| Safe-call receiver chains (`?.` → `.`) | `engine_kotlin_call_metadata_test.go:527` |
| Safe-call alias chains | `engine_kotlin_call_metadata_test.go:578` |
| Dotted property alias chains | `engine_kotlin_call_metadata_test.go:630` |
| Primary constructor property receivers | `engine_kotlin_call_metadata_test.go:682` |
| Lazy delegated property `call_kind` | `engine_kotlin_lazy_property_test.go:11` |

### Smart-cast flow

| Test | File |
|---|---|
| `if (x is T)` braced consequent | `engine_kotlin_smart_cast_test.go:11` |
| `if (x is T)` unbraced consequent | `engine_kotlin_ast_test.go:170` |
| Smart-cast does not leak across branches | `engine_kotlin_ast_test.go:57` |
| `when (subject) { is T -> }` | `engine_kotlin_smart_cast_test.go:60` |
| Generic smart-cast (`ServiceBox<Service>`) | `engine_kotlin_smart_cast_test.go:118` |

### Scope-function transparency

| Test | File |
|---|---|
| `.apply { }` in assignment chain | `engine_kotlin_scope_function_test.go:11` |
| `.also { }` in assignment chain | `engine_kotlin_scope_function_test.go:59` |
| `.apply { }.info()` inline chain | `engine_kotlin_scope_function_test.go:107` |

### Function return-type aliasing and chains

| Test | File |
|---|---|
| Same-file return type alias | `engine_kotlin_function_return_alias_test.go:11` |
| Alias chain through locals | `engine_kotlin_function_return_alias_test.go:60` |
| Nullable return type alias | `engine_kotlin_function_return_alias_test.go:110` |
| Generic return type alias | `engine_kotlin_function_return_alias_test.go:159` |
| Function return receiver chains | `engine_kotlin_function_return_alias_test.go:208` |
| Nested function return assignment | `engine_kotlin_function_return_alias_test.go:259` |
| Constructor root receiver chains | `engine_kotlin_function_return_alias_test.go:317` |
| Parenthesized receiver chains | `engine_kotlin_function_return_alias_test.go:371` |
| Sibling file return types | `engine_kotlin_function_return_alias_test.go:428` |
| Parent directory sibling return types | `engine_kotlin_function_return_alias_test.go:493` |
| Sibling file alias chains | `engine_kotlin_function_return_alias_test.go:559` |
| Package-aware sibling selection (same vs other package) | `engine_kotlin_function_return_alias_test.go:616` |
| Cross-grandparent directory return types | `engine_kotlin_function_return_alias_test.go:694` |
| Cross-file multi-package return types | `engine_kotlin_function_return_alias_test.go:759` |
| Parenthesized cross-file return types | `engine_kotlin_function_return_alias_test.go:855` |
| Package-aware sibling directories | `engine_kotlin_function_return_package_test.go:11` |
| Deeper package directory sibling return types | `engine_kotlin_function_return_package_test.go:89` |

### Dead-code roots

| Test | File |
|---|---|
| All 16 of 19 root kinds verified | `kotlin_dead_code_roots_test.go:11` |
| Non-root functions do not get kinds | `kotlin_dead_code_roots_test.go:116-132` |
| Deadcode fixture expected roots | `kotlin_dead_code_roots_test.go:135` |
| Multiline annotations still classify correctly | `kotlin_dead_code_roots_test.go:164` |

### Repository boundary

| Test | File |
|---|---|
| PreScan stays within repoRoot | `engine_kotlin_repo_boundary_test.go:12` |
| Parse stays within repoRoot | `engine_kotlin_repo_boundary_test.go:49` |

### Golden fixture gate

| Test | File |
|---|---|
| `TestKotlinComprehensiveSymbolExtractionGate` — asserts classes, functions with class context, interfaces, interface methods, calls | `engine_kotlin_swift_symbol_gate_test.go:17` |

## Unverified / Claimed-but-Untested Constructs

Three dead-code root kinds are claimed in source but have **no test assertion**:

1. **`kotlin.spring_configuration_properties_class`** — `dead_code_roots.go:37-39`.
   Triggered by `@ConfigurationProperties` annotation on a class.

2. **`kotlin.spring_event_listener_method`** — `dead_code_roots.go:91-93`.
   Triggered by `@EventListener` annotation on a function.

3. **`kotlin.junit_lifecycle_method`** — `dead_code_roots.go:104-106`.
   Triggered by `@BeforeEach`/`@AfterEach`/`@BeforeAll`/`@AfterAll` annotations.

Additionally, the following aspects are claimed in documentation but lack focused
tests:

- **Enum class declarations** — `doc.go` mentions enums, `ast_declarations.go`
  does not distinguish enum bodies specially. No test asserts enum-specific
  behavior or the `enum_class_body` node kind.
- **`decorators` field** — always initialized to an empty `[]string{}` slice
  (`ast_functions.go:33`). No test populates it with actual decorator values.
- **`IndexSource` path** — `ast_functions.go:69-71` stores `firstLineText` when
  `options.IndexSource` is true. Only `kotlin_dead_code_roots_test.go:94` passes
  `IndexSource: true` but does not assert the `source` field value.
- **`Schedules` annotation** — listed alongside `Scheduled` in
  `dead_code_roots.go:94` (`kotlinHasAnyAnnotation(annotations, "Scheduled",
  "Schedules")`). Only `@Scheduled` is tested; `@Schedules` is not.
- **Cyclomatic complexity for `catch_block`** — `complexity.go:23` includes
  `catch_block` in the branch set. No Kotlin-specific try/catch complexity test
  exists (shared tests may cover this, but Kotlin has no own try/catch fixture).
- **`when_entry` catch-all exclusion for complexity** — `complexity.go:37`
  declares `when_entry` as the catch-all kind. The shared complexity walker
  tests this, but Kotlin has no language-specific when-expression complexity
  fixture.

## Edge Cases Considered

| Edge case | Test reference |
|---|---|
| Smart-cast narrows type inside guarded block | `engine_kotlin_ast_test.go:57` |
| Smart-cast does not leak to sibling statements after block close | `engine_kotlin_ast_test.go:57` |
| Smart-cast with unbraced (concise) consequent | `engine_kotlin_ast_test.go:170` |
| Nested class properties isolated from outer class | `engine_kotlin_treesitter_test.go:59` |
| Empty source / nil parser / nil tree return errors | `ast_declarations.go:14-17` (error vars used in `walkFile`) |
| Anonymous companion object defaults name to `"Companion"` | `ast_declarations.go:228-229` |
| Safe-call operators (`?.`) normalized to plain dots for inference | `engine_kotlin_call_metadata_test.go:527` |
| Scope functions `.also`/`.apply` stripped from receiver chains | `engine_kotlin_scope_function_test.go:11,59,107` |
| Constructor vs bare-call disambiguation via `knownTypeNames` vs `localTypeNames` | `engine_kotlin_ast_test.go:128`, `engine_kotlin_imported_bare_call_test.go:17` |
| Chain-receiver detection prevents double-emission of `x().y()` calls | `ast_calls.go:134-163` (exercised by navigation-call tests) |
| Package-boundary enforcement for sibling return-type collection | `engine_kotlin_function_return_alias_test.go:616`, `engine_kotlin_function_return_package_test.go:11,89` |
| Ambiguous sibling return types (conflicting types) discarded | `repository_returns.go:79-83` (exercised by multi-package tests with conflicts) |
| RepoRoot boundary prevents scanning outside repository | `engine_kotlin_repo_boundary_test.go:12,49` |
| Multiline class declarations with wrapped constructor parameters | `engine_kotlin_treesitter_test.go:11` |
| Multiline annotations for dead-code root classification | `kotlin_dead_code_roots_test.go:164` |
| Nullable types (`?`) stripped during canonicalization | `type_reference.go:8-14`, `engine_kotlin_function_return_alias_test.go:110` |
| Generic type parameter substitution (`T` → concrete type) | `type_reference.go:69-97`, `engine_kotlin_smart_cast_test.go:118` |
| Parenthesized receiver chain normalization | `engine_kotlin_function_return_alias_test.go:371,855` |
| Delegated property (`by lazy`) type inference and `call_kind` | `engine_kotlin_lazy_property_test.go:11` |
| Cast receiver types via `as` expression (variable and inline) | `engine_kotlin_call_metadata_test.go:124,171` |
| Interface method set aggregation for override/implementation classification | `kotlin_dead_code_roots_test.go:11` |
| `@Scheduled` double-count prevention (single `Scheduled` also matched by `Schedules` or-query) | `kotlin_dead_code_roots_test.go:111` |

## Edge Cases NOT Considered

1. **`@ConfigurationProperties` dead-code root** — no test fixture with this
   Spring Boot annotation on a class.
2. **`@EventListener` dead-code root** — no test fixture with this Spring
   annotation on a method.
3. **`@BeforeEach`/`@AfterEach`/`@BeforeAll`/`@AfterAll` dead-code root** — JUnit
   lifecycle annotations claimed but untested.
4. **`@Schedules` container annotation** — grouped scheduling annotation claimed
   but only `@Scheduled` is tested.
5. **Enum classes** — no dedicated enum declaration test. The golden fixture
   gate (`engine_kotlin_swift_symbol_gate_test.go:28-32`) includes sealed class
   variants (`Success`/`Failure`) but does not assert enum-specific behavior.
6. **Abstract classes and abstract functions** — no test asserts abstract class
   handling (though they parse as regular classes).
7. **Sealed interfaces** — Kotlin 1.5+ sealed interfaces not tested.
8. **`try`/`catch`/`finally` expressions** — not handled by the walker (no
   `try_expression` case in `walkNode`). Catch blocks only appear in complexity
   branch set but the try expression itself is never entered.
9. **Lambda expressions as standalone constructs** — general lambdas not handled
   in `walkNode`. Only the lazy-delegate path and scope-function lambdas are
   exercised.
10. **Annotation classes** — not handled by the walker.
11. **Type aliases** — not handled by the walker.
12. **Inline/value classes** — not handled.
13. **Destructuring declarations** — not handled.
14. **Function types as parameters** — not tested.
15. **Primary constructor visibility modifiers** — not tested (e.g., `class C
    internal constructor()`).
16. **`init` blocks** — not handled by the walker (no `init` case).
17. **When expression with non-identifier subject** — only `when (identifier)`
    tested. `when (expr)` with complex expression not tested.
18. **Negated smart-cast (`!is`)** — only positive `is` tested.
19. **Concurrent sibling-file parsing** — `repository_returns.go` reads sibling
    files sequentially; no test for concurrent safety.
20. **Files with no package declaration** — `kotlinPackageNameFromTree` returns
    `""` for files without `package_header`, but sibling return-type collection
    with empty package name behavior is untested.
21. **Files with parse errors in sibling collection** — `repository_returns.go`
    propagates errors; no test injects a malformed sibling file.
22. **Very deeply nested chains** — all call_metadata tests use 2-3 level chains.
    No stress test for deeply nested receiver chains (10+ levels).

## Verdict

**Deep**

The Kotlin parser is among the most thoroughly tested language parsers in Eshu.
Sixteen test files with ~54 test functions cover: every payload bucket, both
call emission paths (bare and navigation), infix calls, constructor detection,
import handling, all receiver-type inference paths (local variable, class
property, function return, cast, smart-cast, lazy delegate, safe-call, object,
companion, primary constructor), scope-function transparency, smart-cast
scoping (braced, unbraced, when-expression, generic), sibling-file return-type
collection with package boundaries, multi-directory lookups, repository boundary
enforcement, dead-code root classification for 16 of 19 claimed kinds, golden
fixture corpus walks, and pre-scan name extraction. The remaining gaps are three
untested dead-code root kinds and a handful of unexercised Kotlin grammar
constructs (enums, try/catch, lambdas, type aliases).

## Recommended Actions

1. **Add tests for the 3 untested dead-code root kinds:**
   - `kotlin.spring_configuration_properties_class` — add a `@ConfigurationProperties` class fixture.
   - `kotlin.spring_event_listener_method` — add an `@EventListener` method fixture.
   - `kotlin.junit_lifecycle_method` — add fixtures with `@BeforeEach`/`@AfterEach`/`@BeforeAll`/`@AfterAll`.

2. **Add a test for `@Schedules` annotation** — extend the existing dead-code
   roots test to include a `@Schedules(@Scheduled(...))` annotation.

3. **Add an enum class test** — parse a file containing enum classes and assert
   they appear in the `classes` bucket with correct names.

4. **Add a `try`/`catch`/`finally` test** — if `try_expression` is intended to
   be extracted, add the `try_expression` case to `walkNode` and a test fixture.
   At minimum, document `try_expression` as intentionally unhandled if that is
   the design decision.

5. **Add a general lambda test** — beyond `by lazy` delegates, test that calls
   inside standalone lambdas are extracted.

6. **Add an `IndexSource` assertion** — the `kotlin_dead_code_roots_test.go:94`
   test passes `IndexSource: true` but does not assert the `source` field.
   Add an explicit assertion.

7. **Add a `decorators` test** — if `decorators` is meant to carry actual values
   (e.g., annotations), populate it and test. If it is intentionally always
   empty, document that in `ast_functions.go`.

8. **Add a `when`-expression complexity test** — a Kotlin-specific fixture that
   verifies the catch-all `else` arm in a `when` expression does not add a
   decision point, confirming the shared catch-all exclusion works correctly
   for Kotlin's `when_entry` node kind.

9. **Document intentionally unhandled constructs** — add a section to
   `README.md` or `doc.go` listing constructs the AST walker intentionally skips
   (try expressions, init blocks, annotation classes, type aliases, etc.) with
   rationale.
