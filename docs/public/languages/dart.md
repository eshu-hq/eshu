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
| Library imports | `library-imports` | supported | `imports` | `name, line_number` | `relationship:IMPORTS` | `go/internal/parser/engine_long_tail_test.go::TestDefaultEngineParsePathDartFixtures` | Compose-backed fixture verification | - |
| Library exports | `library-exports` | supported | `imports` | `name, line_number` | `relationship:IMPORTS` | `go/internal/parser/engine_long_tail_test.go::TestDefaultEngineParsePathDartFixtures` | Compose-backed fixture verification | - |
| Function calls | `function-calls` | supported | `function_calls` | `name, line_number` | `relationship:CALLS` | `go/internal/parser/engine_long_tail_test.go::TestDefaultEngineParsePathDartFixtures` | Compose-backed fixture verification | - |
| Local variable declarations | `local-variable-declarations` | supported | `variables` | `name, line_number` | `node:Variable` | `go/internal/parser/engine_long_tail_test.go::TestDefaultEngineParsePathDartFixtures` | Compose-backed fixture verification | - |
| Top-level variable declarations | `top-level-variable-declarations` | supported | `variables` | `name, line_number` | `node:Variable` | `go/internal/parser/engine_long_tail_test.go::TestDefaultEngineParsePathDartFixtures` | Compose-backed fixture verification | - |

## Known Limitations
- Named constructors (`ClassName.named(...)`) are captured as function symbols
  with `class_context`, but the parser does not resolve constructor tear-offs
- Cascade notation (`..method()`) is not tracked as a distinct call chain
- `part`/`part of` directives are not modeled as import relationships

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

Dogfood evidence for Issue #98 used isolated Docker Compose project names
against `flutter/flutter` and `dart-lang/http`. Both runs returned
`truth.level=derived`, `dead_code_language_maturity.dart=derived`, and the six
modeled Dart root kinds through `/api/v0/code/dead-code`.
