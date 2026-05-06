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
- Named constructors (`ClassName.named(...)`) are captured under the constructor name only
- Cascade notation (`..method()`) is not tracked as a distinct call chain
- `part`/`part of` directives are not modeled as import relationships
