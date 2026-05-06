# Haskell Parser

This page tracks the checked-in Go parser contract in the current repository state.
Canonical implementation: `go/internal/parser/registry.go` plus the entrypoint and tests listed below.

## Parser Contract
- Language: `haskell`
- Family: `language`
- Parser: `DefaultEngine (haskell)`
- Entrypoint: `go/internal/parser/perl_haskell_language.go`
- Fixture repo: `tests/fixtures/ecosystems/haskell_comprehensive/`
- Unit test suite: `go/internal/parser/engine_long_tail_test.go`
- Integration validation: compose-backed fixture verification (see `../reference/local-testing.md`)

## Capability Checklist
| Capability | ID | Status | Extracted Bucket/Key | Required Fields | Graph Surface | Unit Coverage | Integration Coverage | Rationale |
|-----------|----|--------|------------------------|-----------------|---------------|---------------|----------------------|-----------|
| Function declarations | `function-declarations` | supported | `functions` | `name, line_number` | `node:Function` | `go/internal/parser/engine_long_tail_test.go::TestDefaultEngineParsePathHaskellFixtures` | Compose-backed fixture verification | - |
| Initializer declarations | `initializer-declarations` | supported | `functions` | `name, line_number` | `node:Function` | `go/internal/parser/engine_long_tail_test.go::TestDefaultEngineParsePathHaskellFixtures` | Compose-backed fixture verification | - |
| Type classes | `type-classes` | supported | `classes` | `name, line_number` | `node:Class` | `go/internal/parser/engine_long_tail_test.go::TestDefaultEngineParsePathHaskellFixtures` | Compose-backed fixture verification | - |
| Data types (struct-like) | `data-types-struct-like` | supported | `classes` | `name, line_number` | `node:Class` | `go/internal/parser/engine_long_tail_test.go::TestDefaultEngineParsePathHaskellFixtures` | Compose-backed fixture verification | - |
| Enumerations | `enumerations` | supported | `classes` | `name, line_number` | `node:Class` | `go/internal/parser/engine_long_tail_test.go::TestDefaultEngineParsePathHaskellFixtures` | Compose-backed fixture verification | - |
| Protocols/typeclasses | `protocols-typeclasses` | supported | `classes` | `name, line_number` | `node:Class` | `go/internal/parser/engine_long_tail_test.go::TestDefaultEngineParsePathHaskellFixtures` | Compose-backed fixture verification | - |
| Import declarations | `import-declarations` | supported | `imports` | `name, line_number` | `relationship:IMPORTS` | `go/internal/parser/engine_long_tail_test.go::TestDefaultEngineParsePathHaskellFixtures` | Compose-backed fixture verification | - |
| Function call expressions | `function-call-expressions` | supported | `function_calls` | `name, line_number` | `relationship:CALLS` | `go/internal/parser/engine_long_tail_test.go::TestDefaultEngineParsePathHaskellFixtures` | Compose-backed fixture verification | - |
| Property/binding declarations | `property-binding-declarations` | supported | `variables` | `name, line_number` | `node:Variable` | `go/internal/parser/engine_long_tail_test.go::TestDefaultEngineParsePathHaskellFixtures` | Compose-backed fixture verification | - |

## Known Limitations
- Type class instances are not modeled as inheritance relationships
- Where-clauses and let-bindings define local names that are not separately graphed
- Point-free style definitions may result in functions with no parameter information
