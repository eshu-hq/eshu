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
| Function declarations | `function-declarations` | supported | `functions` | `name, line_number` | `node:Function` | `go/internal/parser/haskell/parser_test.go::TestParseCapturesHaskellBuckets` | Compose-backed fixture verification | - |
| Initializer declarations | `initializer-declarations` | supported | `functions` | `name, line_number` | `node:Function` | `go/internal/parser/engine_long_tail_test.go::TestDefaultEngineParsePathHaskellFixtures` | Compose-backed fixture verification | - |
| Type classes | `type-classes` | supported | `classes` | `name, line_number` | `node:Class` | `go/internal/parser/haskell/parser_test.go::TestParseCapturesHaskellDeadCodeRootsAndCalls` | Compose-backed fixture verification | - |
| Data types (struct-like) | `data-types-struct-like` | supported | `classes` | `name, line_number` | `node:Class` | `go/internal/parser/haskell/parser_test.go::TestParseCapturesHaskellDeadCodeRootsAndCalls` | Compose-backed fixture verification | - |
| Enumerations | `enumerations` | supported | `classes` | `name, line_number` | `node:Class` | `go/internal/parser/engine_long_tail_test.go::TestDefaultEngineParsePathHaskellFixtures` | Compose-backed fixture verification | - |
| Protocols/typeclasses | `protocols-typeclasses` | supported | `classes` | `name, line_number` | `node:Class` | `go/internal/parser/haskell/parser_test.go::TestParseCapturesHaskellDeadCodeRootsAndCalls` | Compose-backed fixture verification | - |
| Import declarations | `import-declarations` | supported | `imports` | `name, line_number` | `relationship:IMPORTS` | `go/internal/parser/haskell/parser_test.go::TestParseCapturesHaskellDeadCodeRootsAndCalls` | Compose-backed fixture verification | - |
| Function call expressions | `function-call-expressions` | supported | `function_calls` | `name, line_number` | `relationship:CALLS` | `go/internal/parser/haskell/parser_test.go::TestParseCapturesHaskellDeadCodeRootsAndCalls` | Compose-backed fixture verification | Bounded lexical calls from function right-hand sides |
| Property/binding declarations | `property-binding-declarations` | supported | `variables` | `name, line_number` | `node:Variable` | `go/internal/parser/engine_long_tail_test.go::TestDefaultEngineParsePathHaskellFixtures` | Compose-backed fixture verification | - |

## Dead-code Support

Haskell dead-code support is `derived`. Parser metadata marks `main`, explicit
module-exported functions and types, typeclass method declarations, and instance
methods as root kinds consumed by the dead-code query surface. Function-call
evidence is lexical and bounded to definition right-hand sides, so it supports
triage but does not claim compiler-equivalent Haskell reachability.

Exact cleanup remains blocked by Template Haskell expansion, CPP conditional
compilation, Cabal component resolution, implicit module export surfaces,
typeclass dispatch, module re-export resolution, and foreign-function interface
callbacks.

## Known Limitations
- Type class instances are not modeled as inheritance relationships
- Modules without explicit export lists do not automatically root every
  top-level declaration as public API
- Where-clauses and let-bindings define local names that are not separately
  graphed
- Point-free style definitions may result in functions with no parameter information
