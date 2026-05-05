# Swift Parser

This page tracks the checked-in Go parser contract in the current repository state.
Canonical implementation: `go/internal/parser/registry.go` plus the entrypoint and tests listed below.

## Parser Contract
- Language: `swift`
- Family: `language`
- Parser: `DefaultEngine (swift)`
- Entrypoint: `go/internal/parser/swift_language.go`
- Fixture repo: `tests/fixtures/ecosystems/swift_comprehensive/`
- Unit test suite: `go/internal/parser/engine_swift_semantics_test.go`
- Integration validation: compose-backed fixture verification (see `../reference/local-testing.md`)

## Capability Checklist
| Capability | ID | Status | Extracted Bucket/Key | Required Fields | Graph Surface | Unit Coverage | Integration Coverage | Rationale |
|-----------|----|--------|------------------------|-----------------|---------------|---------------|----------------------|-----------|
| Functions | `functions` | supported | `functions` | `name, line_number` | `node:Function` | `go/internal/parser/engine_swift_semantics_test.go::TestDefaultEngineParsePathSwiftEmitsBasesAndFunctionArgs` | Compose-backed fixture verification | - |
| Initializers (`init`) | `initializers-init` | supported | `functions` | `name, line_number` | `node:Function` | `go/internal/parser/engine_swift_semantics_test.go::TestDefaultEngineParsePathSwiftEmitsImportAndCallMetadata` | Compose-backed fixture verification | - |
| Classes | `classes` | supported | `classes` | `name, line_number` | `node:Class` | `go/internal/parser/engine_long_tail_test.go::TestDefaultEngineParsePathSwiftFixtures` | Compose-backed fixture verification | - |
| Structs | `structs` | supported | `structs` | `name, line_number` | `node:Struct` | `go/internal/parser/engine_long_tail_test.go::TestDefaultEngineParsePathSwiftFixtures` | Compose-backed fixture verification | - |
| Enums | `enums` | supported | `enums` | `name, line_number` | `node:Enum` | `go/internal/parser/engine_long_tail_test.go::TestDefaultEngineParsePathSwiftFixtures` | Compose-backed fixture verification | - |
| Protocols | `protocols` | supported | `protocols` | `name, line_number` | `node:Protocol` | `go/internal/parser/engine_swift_semantics_test.go::TestDefaultEngineParsePathSwiftInfersReceiverCallTypesAndEmitsProtocols` | Compose-backed fixture verification | - |
| Actors | `actors` | supported | `classes` | `name, line_number` | `node:Class` | `go/internal/parser/engine_long_tail_test.go::TestDefaultEngineParsePathSwiftFixtures` | Compose-backed fixture verification | - |
| Imports | `imports` | supported | `imports` | `name, line_number` | `relationship:IMPORTS` | `go/internal/parser/engine_swift_semantics_test.go::TestDefaultEngineParsePathSwiftEmitsImportAndCallMetadata` | Compose-backed fixture verification | - |
| Function calls | `function-calls` | supported | `function_calls` | `name, line_number` | `relationship:CALLS` | `go/internal/parser/engine_swift_semantics_test.go::TestDefaultEngineParsePathSwiftEmitsImportAndCallMetadata` | Compose-backed fixture verification | - |
| Property declarations | `property-declarations` | supported | `variables` | `name, line_number` | `node:Variable` | `go/internal/parser/engine_swift_semantics_test.go::TestDefaultEngineParsePathSwiftEmitsVariableContextAndTypeMetadata` | Compose-backed fixture verification | - |

## Known Limitations
- Property wrappers are not tracked as distinct decorators
- `@objc` and dynamic dispatch attributes are not modeled in the graph
- Computed property bodies are not traversed for embedded function calls
