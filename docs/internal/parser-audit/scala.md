# Scala Parser Audit

## Overview
The Scala parser (`go/internal/parser/scala/`) is a tree-sitter-backed language adapter that extracts classes, objects (stored under "classes"), traits, functions, variables, imports, and call expressions from Scala source files. It also computes cyclomatic complexity per function and emits syntax-backed `dead_code_root_kinds` metadata for framework-specific entry points (Play controllers, Akka actors, JUnit tests, ScalaTest suites, lifecycle callbacks, trait/override/main methods, and App objects). The parser is self-contained within its package and does not import the parent `internal/parser` package. All core parsing, pre-scanning, and dead-code logic live across 4 Go source files totaling ~517 lines.

## Claimed Constructs
| Construct | Source | Location |
|---|---|---|
| classes | `doc.go:6` ("Parse returns classes...") | `doc.go:6` |
| objects | `doc.go:6`, `README.md:9` ("extracts Scala classes, objects, traits...") | `doc.go:6`, `README.md:9` |
| traits | `doc.go:6`, `README.md:9` | `doc.go:6`, `README.md:9` |
| functions | `doc.go:6`, `README.md:9`, `language.go:15` ("Parse extracts Scala declarations, imports, variables, and calls.") | `doc.go:6`, `README.md:9`, `language.go:15` |
| variables | `doc.go:6`, `README.md:9`, `language.go:54-58` (`val_definition`, `var_definition`) | `doc.go:6`, `README.md:9`, `language.go:54-58` |
| imports | `doc.go:6`, `README.md:9`, `language.go:59-68` (`import_declaration`) | `doc.go:6`, `README.md:9`, `language.go:59-68` |
| calls | `doc.go:6`, `README.md:9`, `language.go:69-70` (`call_expression`) | `doc.go:6`, `README.md:9`, `language.go:69-70` |
| dead_code_root_kinds metadata | `doc.go:7` ("bounded dead-code root metadata"), `README.md:10,13-18` | `doc.go:7`, `README.md:10,13-18` |
| scala.main_method | `README.md:13` ("main methods"), `dead_code_roots.go:39-41` | `README.md:13`, `dead_code_roots.go:39-41` |
| scala.app_object | `README.md:14` ("objects extending App"), `dead_code_roots.go:18-21` | `README.md:14`, `dead_code_roots.go:18-21` |
| scala.trait_type | `README.md:14` ("traits"), `dead_code_roots.go:16-17` | `README.md:14`, `dead_code_roots.go:16-17` |
| scala.trait_method | `README.md:14-15` ("trait methods"), `dead_code_roots.go:42-44` | `README.md:14-15`, `dead_code_roots.go:42-44` |
| scala.trait_implementation_method | `README.md:15` ("same-file trait implementations"), `dead_code_roots.go:48-50` | `README.md:15`, `dead_code_roots.go:48-50` |
| scala.override_method | `README.md:15` ("overrides"), `dead_code_roots.go:45-47` | `README.md:15`, `dead_code_roots.go:45-47` |
| scala.play_controller_action | `README.md:16` ("Play controller actions"), `dead_code_roots.go:51-53` | `README.md:16`, `dead_code_roots.go:51-53` |
| scala.akka_actor_receive | `README.md:16` ("Akka actor receive methods"), `dead_code_roots.go:54-56` | `README.md:16`, `dead_code_roots.go:54-56` |
| scala.lifecycle_callback_method | `README.md:16-17` ("lifecycle callbacks"), `dead_code_roots.go:57-59` | `README.md:16-17`, `dead_code_roots.go:57-59` |
| scala.junit_test_method | `README.md:17` ("JUnit methods"), `dead_code_roots.go:60-62` | `README.md:17`, `dead_code_roots.go:60-62` |
| scala.scalatest_suite_class | `README.md:17-18` ("ScalaTest suite classes"), `dead_code_roots.go:23-27` | `README.md:17-18`, `dead_code_roots.go:23-27` |
| cyclomatic_complexity | `complexity.go:30-32` (emitted per function), `AGENTS.md` (parser/AGENTS.md cyclomatic complexity section) | `complexity.go:30-32`, `language.go:194` |
| class_context (on functions) | `language.go:196-198` (`nearestNamedAncestor`) | `language.go:196-198` |
| decorators (on functions) | `language.go:192` (emitted as `[]string{}`) | `language.go:192` |
| framework_semantics | `language.go:77` (hardcoded `{"frameworks": []}`) | `language.go:77` |
| PreScan (import-map names) | `README.md:11`, `language.go:82-91` | `README.md:11`, `language.go:82-91` |

## Verified-by-Test Constructs
| Construct | Test File | Test Function |
|---|---|---|
| classes | `engine_managed_oo_test.go:284-285` | `TestDefaultEngineParsePathScala` |
| traits | `engine_managed_oo_test.go:286` | `TestDefaultEngineParsePathScala` |
| functions | `engine_managed_oo_test.go:287-289` | `TestDefaultEngineParsePathScala` |
| variables | `engine_managed_oo_test.go:290` | `TestDefaultEngineParsePathScala` |
| imports | `engine_managed_oo_test.go:291` | `TestDefaultEngineParsePathScala` |
| function_calls | `engine_managed_oo_test.go:292` | `TestDefaultEngineParsePathScala` |
| class_context (on functions) | `engine_managed_oo_test.go:293-294` | `TestDefaultEngineParsePathScala` |
| lang = "scala" | `engine_managed_oo_test.go:280-281` | `TestDefaultEngineParsePathScala` |
| scala.trait_type | `scala_dead_code_roots_test.go:91,131` | `TestDefaultEngineParsePathScalaEmitsDeadCodeRootKinds`, `TestDefaultEngineParsePathScalaDeadCodeFixtureExpectedRoots` |
| scala.trait_method | `scala_dead_code_roots_test.go:92,132` | `TestDefaultEngineParsePathScalaEmitsDeadCodeRootKinds`, `TestDefaultEngineParsePathScalaDeadCodeFixtureExpectedRoots` |
| scala.override_method | `scala_dead_code_roots_test.go:93` | `TestDefaultEngineParsePathScalaEmitsDeadCodeRootKinds` |
| scala.trait_implementation_method | `scala_dead_code_roots_test.go:94-95,133` | `TestDefaultEngineParsePathScalaEmitsDeadCodeRootKinds`, `TestDefaultEngineParsePathScalaDeadCodeFixtureExpectedRoots` |
| scala.play_controller_action | `scala_dead_code_roots_test.go:96,134` | `TestDefaultEngineParsePathScalaEmitsDeadCodeRootKinds`, `TestDefaultEngineParsePathScalaDeadCodeFixtureExpectedRoots` |
| scala.akka_actor_receive | `scala_dead_code_roots_test.go:97,135` | `TestDefaultEngineParsePathScalaEmitsDeadCodeRootKinds`, `TestDefaultEngineParsePathScalaDeadCodeFixtureExpectedRoots` |
| scala.lifecycle_callback_method | `scala_dead_code_roots_test.go:98` | `TestDefaultEngineParsePathScalaEmitsDeadCodeRootKinds` |
| scala.junit_test_method | `scala_dead_code_roots_test.go:99,136` | `TestDefaultEngineParsePathScalaEmitsDeadCodeRootKinds`, `TestDefaultEngineParsePathScalaDeadCodeFixtureExpectedRoots` |
| scala.scalatest_suite_class | `scala_dead_code_roots_test.go:100,137` | `TestDefaultEngineParsePathScalaEmitsDeadCodeRootKinds`, `TestDefaultEngineParsePathScalaDeadCodeFixtureExpectedRoots` |
| scala.app_object | `scala_dead_code_roots_test.go:101,138` | `TestDefaultEngineParsePathScalaEmitsDeadCodeRootKinds`, `TestDefaultEngineParsePathScalaDeadCodeFixtureExpectedRoots` |
| scala.main_method | `scala_dead_code_roots_test.go:102,139` | `TestDefaultEngineParsePathScalaEmitsDeadCodeRootKinds`, `TestDefaultEngineParsePathScalaDeadCodeFixtureExpectedRoots` |
| Negative: non-root functions | `scala_dead_code_roots_test.go:104-112,140-142` | `TestDefaultEngineParsePathScalaEmitsDeadCodeRootKinds`, `TestDefaultEngineParsePathScalaDeadCodeFixtureExpectedRoots` |
| Cyclomatic complexity (straight line = 1) | `engine_cyclomatic_complexity_test.go:134-139` | `TestCyclomaticComplexityPerLanguage` |
| Cyclomatic complexity (branches + boolean = 3) | `engine_cyclomatic_complexity_test.go:141-147` | `TestCyclomaticComplexityPerLanguage` |
| Cyclomatic complexity (catch adds 1) | `engine_cyclomatic_complexity_arms_test.go:59-64` | `TestCyclomaticComplexityCatchAndDefaultArms` |
| Cyclomatic complexity (wildcard is implicit else) | `engine_cyclomatic_complexity_arms_test.go:164-169` | `TestCyclomaticComplexityCatchAndDefaultArms` |
| PreScan (names returned) | `engine_managed_oo_test.go:434-436` | `TestDefaultEnginePreScanPathsManagedOO` |

## Unverified / Claimed-but-Untested Constructs
| Construct | Source | Issue |
|---|---|---|
| `objects` as a distinct entity type | `doc.go:6`, `README.md:9` | `object_definition` nodes are emitted to the "classes" bucket (not a separate "objects" bucket). The README implies they are a distinct type, but the implementation conflates them with classes. |
| `framework_semantics` | `language.go:77` | Always emitted as `{"frameworks": []}`. Never populated with actual frameworks. Not asserted by any test. Appears to be dead code. |
| `decorators` on functions | `language.go:192` | Always emitted as `[]string{}`. Never populated. Not asserted by any test. |
| `IndexSource` option | `language.go:211-213` | `TestDefaultEngineParsePathScalaEmitsDeadCodeRootKinds` passes `IndexSource: true` but does not assert on the `source` field in the output. |
| ScalaTest: `AnyFlatSpec`, `AnyWordSpec`, `AnyFreeSpec`, `AnyFeatureSpec`, `FunSuite`, `FlatSpec`, `WordSpec`, `FreeSpec`, `Specification` | `dead_code_roots.go:23-25` | Only `AnyFunSuite` is tested. 8 of 9 ScalaTest base class names are untested. |
| Play controllers: `BaseController`, `AbstractController`, `Controller` | `dead_code_roots.go:67` | Only `InjectedController` is tested. 3 of 4 Play base controller types are untested. |
| Play action: `EssentialAction` | `dead_code_roots.go:70` | Only `Action` is tested in the method body check. `EssentialAction` is untested. |
| Akka: `AbstractActor` | `dead_code_roots.go:54` | Only `Actor` is tested. `AbstractActor` is untested. |
| Lifecycle: `@PreDestroy` | `dead_code_roots.go:57` | Only `@PostConstruct` is tested. `@PreDestroy` is untested. |
| JUnit: `@ParameterizedTest`, `@RepeatedTest`, `@TestFactory`, `@TestTemplate`, `@org.junit.*` | `dead_code_roots.go:60-61` | Only `@Test` (plain) is tested. 4 standard JUnit annotations plus the `org.junit.` prefix path are untested. |
| `val`/`var` inside function body (module scope filtering) | `language.go:55-57` | The `scalaInsideFunction` helper suppresses val/var extraction for module-scoped files. No test exercises a val inside a function body to confirm exclusion. |
| Empty import name handling | `language.go:61-63` | Code skips empty import names. Not directly tested. |
| Empty call name handling | `language.go:247-249` | Code skips empty call names. Not directly tested. |
| Dependency mode (`isDependency=true`) | `language.go:16` | The `basePayload` fills `isDependency` but no test exercises dependency-mode Scala parsing. |
| `function_declaration` (abstract method) | `language.go:41` | Parsed identically to `function_definition`. No test discriminates between the two. |
| Scala 3 constructs (enums, given/using, extension methods, top-level defs, indentation syntax) | — | Not claimed in docs, but fully absent from both implementation and tests. |

## Edge Cases Considered
| Edge Case | Test Reference | Notes |
|---|---|---|
| Cyclomatic complexity straight-line base case (1) | `engine_cyclomatic_complexity_test.go:134` | `def run(x: Int): Int = x + 1` yields 1 |
| Cyclomatic complexity with `if` and `&&` (3) | `engine_cyclomatic_complexity_test.go:141` | `if (x > 0 && x < 10) 1 else 0` yields 3 |
| Cyclomatic complexity: catch clause adds a decision point | `engine_cyclomatic_complexity_arms_test.go:59` | `try { 1 } catch { case e: Exception => 2 }` yields 2 |
| Cyclomatic complexity: wildcard `case _` as implicit else | `engine_cyclomatic_complexity_arms_test.go:164` | Match with one named case + wildcard yields 2, not 3 |
| Non-root function does NOT get dead_code_root_kinds | `scala_dead_code_roots_test.go:104-112` | `Worker.helper`, `HealthController.helper`, `UnusedHelpers.unusedCleanupCandidate` all nil |
| Same-file disparate traits: class implements trait from separate trait | `scala_dead_code_roots_test.go:94-95` | `Worker extends Runner`, `AuditedWorker extends UsewithLogging` — both correctly detect `trait_implementation_method` |
| Class with `extends` containing type parameters | `scala_dead_code_roots_test.go:18-19` | `ConsoleApp extends App` with no type args; `InjectedController` is a qualified name resolved as just `InjectedController` |
| Private object with private method excluded from roots | `scala_dead_code_roots_test.go:110-112` | `private object UnusedHelpers` + `unusedCleanupCandidate` is nil |

## Edge Cases NOT Considered
- **Empty .scala file**: No test passes an empty file to `Parse`.
- **Parse failure / nil tree**: No test exercises the `parser.Parse` returning nil path.
- **Unicode identifiers**: Scala 3 allows Unicode operators and identifiers (e.g., `∀`, `→`). No Unicode test.
- **Deeply nested definitions**: No test with 3+ levels of nesting (object inside class inside trait).
- **Cyclic trait inheritance**: No test with `trait A extends B` and `trait B extends A`.
- **Import with wildcard**: `import scala.collection._` — no test.
- **Import with renaming**: `import java.util.{List => JList}` — no test.
- **Multiple imports in one import block**: `import scala.collection.{mutable, immutable}` — no test.
- **Multiple import selectors**: The `scalaImportName` function joins all identifiers with `.`; multi-selector syntax may produce incorrect names — untested.
- **Case classes**: `case class Point(x: Int, y: Int)` — no test.
- **Companion objects**: `class Foo` + `object Foo` in same file — no test.
- **Nested classes/objects/traits**: `class Outer { class Inner }` — no test.
- **Package objects**: `package object util` — no test.
- **Self-types**: `trait A { self: B => }` — no test.
- **Lazy vals**: `lazy val x = ...` — no test.
- **Pattern matching in val definitions**: `val (a, b) = pair` — no test.
- **`for` comprehensions with multiple generators/filters**: Cyclomatic complexity for `for` + `if` guard — untested.
- **Scala 3 syntax**: Top-level definitions, significant indentation (braces-less), enums, given/using, extension methods, export clauses — entirely untested (and not claimed, but noteably absent).
- **Large file performance**: No benchmark or large-file validation for Scala parsing.
- **Concurrent parse safety**: No test validates concurrent `Parse` calls.
- **`var_definition` extract**: Only `val_definition` is tested (`version` in `TestDefaultEngineParsePathScala`). `var_definition` has the same code path but is not explicitly tested.

## Verdict
**Moderate**

The Scala parser has solid coverage for its primary claim areas — core symbol extraction (classes, traits, functions, variables, imports, calls) and all 13 dead-code root kinds have at least one positive assertion. Cyclomatic complexity is well-tested across four dedicated cases (straight-line, branches, catch, wildcard). However, the test suite has three gaps that prevent a "deep" rating: (1) 0 tests live in the `scala/` package itself — all tests are in the parent `parser` package driving the parser through `DefaultEngine`, meaning internal helpers like `scalaImplementedTypes`, `scalaShortTypeName`, `scalaHasAnnotation`, and `scalaCollectTypeContracts` have no unit-level coverage; (2) 7 of 13 dead-code root detection paths test only a single variant of multi-variant detection logic; and (3) there are zero edge-case tests for empty input, parse failure, unicode, nested structures, import variants, or Scala 3 constructs.

## Recommended Actions
- Add unit tests in `go/internal/parser/scala/scala_test.go` for internal helpers: `scalaImplementedTypes`, `scalaShortTypeName`, `scalaHasAnnotation`, `scalaCollectTypeContracts`, `scalaIsPlayControllerAction`, `scalaTypeImplementsMethod`, `scalaExtendsAny`, and `scalaExtendsType`.
- Add dead-code root verification for the untested detection variants: `AnyFlatSpec`/`WordSpec`/`FlatSpec`/`FreeSpec`/`Specification` (ScalaTest), `BaseController`/`AbstractController`/`Controller` (Play), `AbstractActor` (Akka), `@PreDestroy` (lifecycle), and `@ParameterizedTest`/`@RepeatedTest`/`@TestFactory`/`@org.junit.Test` (JUnit).
- Add a test for `var_definition` extraction (currently only `val_definition` is tested).
- Add a test for empty `.scala` file to confirm `Parse` returns empty buckets without a nil-tree error.
- Add a test for a nil-tree parse error path.
- Add tests for import edge cases: wildcard import (`_`), import with renaming (`=>`), multi-selector import (`{a, b}`).
- Add a test for `val`/`var` inside a function body with module scope to confirm exclusion.
- Add a test for `IndexSource: true` that asserts the `source` field is populated.
- Either populate or remove the always-empty `framework_semantics` and `decorators` fields — they are dead data.
- Consider whether `object_definition` should be emitted to a distinct "objects" bucket (or update the README to clarify that objects are classified as classes).
- Wire the existing `tests/fixtures/ecosystems/scala_comprehensive/` and `tests/fixtures/sample_projects/sample_project_scala/` fixtures to integration-level tests — they currently have zero test references.
- If Scala 3 support is intended, add a dedicated Scala 3 fixture and tests covering enums, given/using, extension methods, top-level definitions, and indentation syntax.
