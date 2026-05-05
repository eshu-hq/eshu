# C# Parser

This page tracks the checked-in Go parser contract in the current repository state.
Canonical implementation: `go/internal/parser/registry.go` plus the entrypoint and tests listed below.

## Parser Contract
- Language: `csharp`
- Family: `language`
- Parser: `DefaultEngine (c_sharp)`
- Entrypoint: `go/internal/parser/csharp_language.go`
- Fixture repo: `tests/fixtures/ecosystems/csharp_comprehensive/`
- Unit test suite: `go/internal/parser/engine_managed_oo_test.go`
- Integration validation: compose-backed fixture verification (see `../reference/local-testing.md`)

## Capability Checklist
| Capability | ID | Status | Extracted Bucket/Key | Required Fields | Graph Surface | Unit Coverage | Integration Coverage | Rationale |
|-----------|----|--------|------------------------|-----------------|---------------|---------------|----------------------|-----------|
| Methods | `methods` | supported | `functions` | `name, line_number` | `node:Function` | `go/internal/parser/engine_managed_oo_test.go::TestDefaultEngineParsePathCSharp` | Compose-backed fixture verification | - |
| Constructors | `constructors` | supported | `functions` | `name, line_number` | `node:Function` | `go/internal/parser/engine_managed_oo_test.go::TestDefaultEngineParsePathCSharp` | Compose-backed fixture verification | - |
| Local functions | `local-functions` | supported | `functions` | `name, line_number` | `node:Function` | `go/internal/parser/engine_managed_oo_test.go::TestDefaultEngineParsePathCSharpLocalTypes` | Compose-backed fixture verification | - |
| Classes | `classes` | supported | `classes` | `name, line_number` | `node:Class` | `go/internal/parser/engine_managed_oo_test.go::TestDefaultEngineParsePathCSharp` | Compose-backed fixture verification | - |
| Interfaces | `interfaces` | supported | `interfaces` | `name, line_number` | `node:Interface` | `go/internal/parser/engine_managed_oo_test.go::TestDefaultEngineParsePathCSharp` | Compose-backed fixture verification | - |
| Structs | `structs` | supported | `structs` | `name, line_number` | `node:Struct` | `go/internal/parser/engine_managed_oo_test.go::TestDefaultEngineParsePathCSharpLocalTypes` | Compose-backed fixture verification | - |
| Records | `records` | supported | `records` | `name, line_number` | `node:Record` | `go/internal/parser/engine_managed_oo_test.go::TestDefaultEngineParsePathCSharp` | Compose-backed fixture verification | - |
| Enums | `enums` | supported | `enums` | `name, line_number` | `node:Enum` | `go/internal/parser/engine_managed_oo_test.go::TestDefaultEngineParsePathCSharpLocalTypes` | Compose-backed fixture verification | - |
| Properties | `properties` | supported | `properties` | `name, line_number` | `node:Property` | `go/internal/parser/engine_managed_oo_test.go::TestDefaultEngineParsePathCSharp` | Compose-backed fixture verification | - |
| Using directives | `using-directives` | supported | `imports` | `name, line_number` | `relationship:IMPORTS` | `go/internal/parser/engine_managed_oo_test.go::TestDefaultEngineParsePathCSharp` | Compose-backed fixture verification | - |
| Method invocations | `method-invocations` | supported | `function_calls` | `name, line_number` | `relationship:CALLS` | `go/internal/parser/engine_managed_oo_test.go::TestDefaultEngineParsePathCSharp` | Compose-backed fixture verification | - |
| Object creation | `object-creation` | supported | `function_calls` | `name, line_number` | `relationship:CALLS` | `go/internal/parser/engine_managed_oo_test.go::TestDefaultEngineParsePathCSharp` | Compose-backed fixture verification | - |
| Inheritance (`base_list`) | `inheritance-base-list` | supported | `classes` | `name, line_number, bases` | `relationship:INHERITS` | `go/internal/parser/engine_managed_oo_test.go::TestDefaultEngineParsePathCSharp` | Compose-backed fixture verification | - |

## Known Limitations
- Extension methods are not tagged as extensions in the graph
- Partial class merging across files is not performed
- Nullable reference types (`T?`) not surfaced as distinct type metadata
