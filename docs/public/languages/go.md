# Go Parser

This page tracks the checked-in Go parser contract in the current repository state.
Canonical implementation: `go/internal/parser/registry.go` plus the entrypoint and tests listed below.

## Parser Contract
- Language: `go`
- Family: `language`
- Parser: `DefaultEngine (go)`
- Entrypoint: `go/internal/parser/go_language.go`
- Fixture repo: `tests/fixtures/ecosystems/go_comprehensive/`
- Unit test suite: `go/internal/parser/engine_test.go`
- Integration validation: compose-backed fixture verification (see `../reference/local-testing.md`)

## Capability Checklist
| Capability | ID | Status | Extracted Bucket/Key | Required Fields | Graph Surface | Unit Coverage | Integration Coverage | Rationale |
|-----------|----|--------|------------------------|-----------------|---------------|---------------|----------------------|-----------|
| Functions | `functions` | supported | `functions` | `name, line_number` | `node:Function` | `go/internal/parser/engine_test.go::TestDefaultEngineParsePathGo` | Compose-backed fixture verification | - |
| Structs | `structs` | supported | `classes` | `name, line_number` | `node:Class` | `go/internal/parser/engine_test.go::TestDefaultEngineParsePathGo` | Compose-backed fixture verification | - |
| Interfaces | `interfaces` | supported | `interfaces` | `name, line_number` | `node:Interface` | `go/internal/parser/engine_test.go::TestDefaultEngineParsePathGo` | Compose-backed fixture verification | - |
| Imports | `imports` | supported | `imports` | `name, line_number` | `relationship:IMPORTS` | `go/internal/parser/engine_test.go::TestDefaultEngineParsePathGo` | Compose-backed fixture verification | - |
| Function calls | `function-calls` | supported | `function_calls` | `name, line_number` | `relationship:CALLS` | `go/internal/parser/engine_test.go::TestDefaultEngineParsePathGo` | Compose-backed fixture verification | - |
| Variables | `variables` | supported | `variables` | `name, line_number` | `node:Variable` | `go/internal/parser/engine_test.go::TestDefaultEngineParsePathGo` | Compose-backed fixture verification | - |
| Methods (receivers) | `methods-receivers` | supported | `functions` | `name, line_number` | `node:Function` | `go/internal/parser/engine_test.go::TestDefaultEngineParsePathGo` | Compose-backed fixture verification | - |
| Generics | `generics` | supported | `functions` | `name, line_number` | `node:Function` | `go/internal/parser/engine_python_semantics_test.go::TestDefaultEngineParsePathGoRichSemanticMetadata` | Compose-backed fixture verification | - |
| Embedded SQL queries | `embedded-sql-queries` | supported | `embedded_sql_queries` | `function_name, function_line_number, table_name, operation, line_number, api` | `relationship:SQL link hints consumed by sql_links materialization` | `go/internal/parser/go_embedded_sql_test.go::TestDefaultEngineParsePathGoEmbeddedSQLQueries` | Compose-backed fixture verification | - |

## Known Limitations
- Generic type constraints may not be fully captured
- Channel types not separately tracked
