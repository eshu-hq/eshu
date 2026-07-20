# Dart Parser

This page tracks the checked-in Go parser contract in the current repository state.
Canonical implementation: `go/internal/parser/registry.go` plus the entrypoint and tests listed below.

## Parser Contract
- Language: `dart`
- Family: `language`
- Parser: `DefaultEngine (dart)`
- Entrypoint: `go/internal/parser/dart_language.go`
- Fixture repo: `tests/fixtures/ecosystems/dart_comprehensive/`
- Unit test suite: `go/internal/parser/engine_long_tail_test.go`
- Integration validation: compose-backed fixture verification (see `../reference/local-testing.md`)

## Capability Checklist
| Capability | ID | Status | Extracted Bucket/Key | Required Fields | Graph Surface | Unit Coverage | Integration Coverage | Rationale |
|-----------|----|--------|------------------------|-----------------|---------------|---------------|----------------------|-----------|
| Functions | `functions` | supported | `functions` | `name, line_number` | `node:Function` | `go/internal/parser/engine_long_tail_test.go::TestDefaultEngineParsePathDartFixtures` | Compose-backed fixture verification | - |
| Constructors | `constructors` | supported | `functions` | `name, line_number` | `node:Function` | `go/internal/parser/engine_long_tail_test.go::TestDefaultEngineParsePathDartFixtures` | Compose-backed fixture verification | - |
| Classes | `classes` | supported | `classes` | `name, line_number` | `node:Class` | `go/internal/parser/engine_long_tail_test.go::TestDefaultEngineParsePathDartFixtures` | Compose-backed fixture verification | - |
| Mixins | `mixins` | supported | `classes` | `name, line_number` | `node:Class` | `go/internal/parser/engine_long_tail_test.go::TestDefaultEngineParsePathDartFixtures` | Compose-backed fixture verification | - |
| Extensions | `extensions` | supported | `classes` | `name, line_number` | `node:Class` | `go/internal/parser/engine_long_tail_test.go::TestDefaultEngineParsePathDartFixtures` | Compose-backed fixture verification | - |
| Enums | `enums` | supported | `classes` | `name, line_number` | `node:Class` | `go/internal/parser/engine_long_tail_test.go::TestDefaultEngineParsePathDartFixtures` | Compose-backed fixture verification | - |
| Library imports | `library-imports` | supported | `imports` | `name, line_number, import_type=import` | `relationship:IMPORTS` | `go/internal/parser/engine_long_tail_test.go::TestDefaultEngineParsePathDartFixtures` | Compose-backed fixture verification | - |
| Library exports | `library-exports` | supported | `imports` | `name, line_number, import_type=export` | `relationship:IMPORTS` | `go/internal/parser/engine_long_tail_test.go::TestDefaultEngineParsePathDartFixtures` | Compose-backed fixture verification | - |
| Function calls | `function-calls` | supported | `function_calls` | `name, full_name, line_number` | `relationship:CALLS` | `go/internal/parser/engine_long_tail_test.go::TestDefaultEngineParsePathDartFixtures`, `go/internal/parser/dart/calls_ast_test.go` | Compose-backed fixture verification | Call sites are extracted by an AST walk over the call-expression grammar shapes (`selector`/`argument_part`, cascades, `new`/`const` object creation), not a byte scan, so declaration signatures can never be misclassified as call sites. `full_name` is the dotted qualified callee chain (e.g. `f.build`, `fut.then`); genuine recursion (caller calls itself) still emits a real self-edge. |
| Local variable declarations | `local-variable-declarations` | supported | `variables` | `name, line_number` | `node:Variable` | `go/internal/parser/engine_long_tail_test.go::TestDefaultEngineParsePathDartFixtures` | Compose-backed fixture verification | - |
| Top-level variable declarations | `top-level-variable-declarations` | supported | `variables` | `name, line_number` | `node:Variable` | `go/internal/parser/engine_long_tail_test.go::TestDefaultEngineParsePathDartFixtures` | Compose-backed fixture verification | - |

## Known Limitations
- Named constructors (`ClassName.named(...)`) are captured as function symbols
  with `class_context`, but the parser does not resolve constructor tear-offs
- Cascade notation (`..method()`) is tracked as a call site, but the chain's
  receiver is not carried into `full_name` (`b..write("a")..write("b")`
  emits `full_name="write"` for both call sites, not `b.write`), so repeat
  cascade calls to the same method name on one receiver collapse into a
  single `function_calls` row under the full-name dedup
- `part`/`part of` directives are not modeled as import relationships
- `function_calls` reflects real call-site truth: a call-site AST walk
  (`go/internal/parser/dart/calls.go`) replaced an earlier byte-scanner that
  misclassified every function/method/constructor declaration as a call to
  itself, materializing a spurious self-loop `CALLS` edge for every
  declaration in the corpus (eshu-hq/eshu#5332). Declarations and calls are
  disjoint grammar node kinds, so a declaration can no longer be
  misclassified as a call, while genuine recursion (a function calling
  itself) still produces a real self-edge. Call-site detection now runs in the
  same single tree traversal as declaration extraction (folded into
  `dartSyntaxIndex.collect` via `dartCallChain.observe`, eshu-hq/eshu#5350) —
  an output-preserving optimization (byte-identical `function_calls`, guarded by
  the frozen `oracleWalkDartCallSites` differential), not a behavior change.

## Dead-Code Support

Maturity: `derived`.

Parser metadata marks these roots through `dead_code_root_kinds`:

- `dart.main_function` for top-level `main()`
- `dart.constructor` for constructors and named constructors
- `dart.override_method` for methods preceded by `@override`
- `dart.flutter_widget_build` for Flutter widget `build` methods
- `dart.flutter_create_state` for `StatefulWidget.createState`
- `dart.public_library_api` for public `lib/` declarations outside `lib/src/`

Exact cleanup is still blocked by part-file library resolution, conditional
imports and exports, package export surfaces, dynamic dispatch, Flutter route
and lifecycle wiring, generated code, reflection/mirrors, and broad public API
surfaces.

Historical note (not current grade evidence): an Issue #98 dogfood run used
isolated Docker Compose project names against `flutter/flutter` and
`dart-lang/http` and reported `truth.level=derived`,
`dead_code_language_maturity.dart=derived`, and the six modeled Dart root kinds
through `/api/v0/code/dead-code`. That run left no committed, offline-reproducible
artifact, so it does not back a `real-repo-validated` or `supported` grade. Dart's
Real-Repo Validation and End-to-End Indexing grades are `fixture-backed` (see
[Parser Support Matrix](support-maturity.md#grade-definitions)); earning
`real-repo-validated` requires a committed `scripts/` dogfood script plus a
checked-in expected-output snapshot.

## Framework And Library Support

Supported today:

- Flutter widget `build` methods and `StatefulWidget.createState` methods are
  modeled as derived roots.
- Public `lib/` declarations outside `lib/src/`, constructors, named
  constructors, top-level `main`, and overrides are modeled as live API or
  runtime evidence.

Not claimed today:

- Flutter route wiring, generated code, mirrors, conditional imports/exports,
  package export surfaces, and broad dynamic dispatch remain exactness blockers.
- Pub dependency evidence is source-file evidence, not Dart language graph
  reachability. The YAML parser emits hosted `pubspec.yaml` range rows and
  exact hosted `pubspec.lock` version rows for supply-chain impact; git/path,
  private-hosted, override, and mismatched lockfile rows remain partial or
  missing evidence.
