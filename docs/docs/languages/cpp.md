# C++ Parser

This page tracks the checked-in Go parser contract in the current repository state.
Canonical implementation: `go/internal/parser/registry.go` plus the entrypoint and tests listed below.

## Parser Contract
- Language: `cpp`
- Family: `language`
- Parser: `DefaultEngine (cpp)`
- Entrypoint: `go/internal/parser/cpp_language.go`
- Fixture repo: `tests/fixtures/ecosystems/cpp_comprehensive/`
- Unit test suite: `go/internal/parser/engine_systems_test.go`
- Integration validation: compose-backed fixture verification (see `../reference/local-testing.md`)

## Capability Checklist
| Capability | ID | Status | Extracted Bucket/Key | Required Fields | Graph Surface | Unit Coverage | Integration Coverage | Rationale |
|-----------|----|--------|------------------------|-----------------|---------------|---------------|----------------------|-----------|
| Functions | `functions` | supported | `functions` | `name, line_number` | `node:Function` | `go/internal/parser/engine_systems_test.go::TestDefaultEngineParsePathCPP` | Compose-backed fixture verification | - |
| Classes | `classes` | supported | `classes` | `name, line_number` | `node:Class` | `go/internal/parser/engine_systems_test.go::TestDefaultEngineParsePathCPP` | Compose-backed fixture verification | - |
| Structs | `structs` | supported | `structs` | `name, line_number` | `node:Struct` | `go/internal/parser/engine_systems_test.go::TestDefaultEngineParsePathCPP` | Compose-backed fixture verification | - |
| Enums | `enums` | supported | `enums` | `name, line_number` | `node:Enum` | `go/internal/parser/engine_systems_test.go::TestDefaultEngineParsePathCPP` | Compose-backed fixture verification | - |
| Unions | `unions` | supported | `unions` | `name, line_number` | `node:Union` | `go/internal/parser/engine_systems_test.go::TestDefaultEngineParsePathCPP` | Compose-backed fixture verification | - |
| Includes | `includes` | supported | `imports` | `name, line_number` | `relationship:IMPORTS` | `go/internal/parser/engine_systems_test.go::TestDefaultEngineParsePathCPP` | Compose-backed fixture verification | - |
| Function calls | `function-calls` | supported | `function_calls` | `name, line_number` | `relationship:CALLS` | `go/internal/parser/engine_systems_test.go::TestDefaultEngineParsePathCPP` | Compose-backed fixture verification | - |
| Method calls | `method-calls` | supported | `function_calls` | `name, line_number` | `relationship:CALLS` | `go/internal/parser/engine_systems_test.go::TestDefaultEngineParsePathCPP` | Compose-backed fixture verification | - |
| Variables (initialized) | `variables-initialized` | supported | `variables` | `name, line_number` | `node:Variable` | `go/internal/parser/engine_systems_test.go::TestDefaultEngineParsePathCPP` | Compose-backed fixture verification | - |
| Field declarations | `field-declarations` | supported | `variables` | `name, line_number` | `node:Variable` | `go/internal/parser/engine_systems_test.go::TestDefaultEngineParsePathCPP` | Compose-backed fixture verification | - |
| Macros (`#define`) | `macros-define` | supported | `macros` | `name, line_number` | `node:Macro` | `go/internal/parser/engine_systems_test.go::TestDefaultEngineParsePathCPP` | Compose-backed fixture verification | - |
| Lambda assignments | `lambda-assignments` | supported | `functions` | `name, line_number` | `node:Function` | `go/internal/parser/engine_systems_test.go::TestDefaultEngineParsePathCPP` | Compose-backed fixture verification | - |

## Known Limitations
- Template specializations are not separately modeled
- Operator overloads are captured as regular functions without operator context
- Preprocessor-conditional code blocks are parsed as-is without branch tracking
