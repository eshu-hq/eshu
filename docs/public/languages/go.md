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
| net/http route truth | `net-http-route-truth` | supported | `framework_semantics.net_http.route_entries` | `method, path, handler` for exact literal patterns and identifier handlers | `HANDLES_ROUTE` when reducer can resolve the exact handler | `go/internal/parser/go_dead_code_registrations_test.go::TestDefaultEngineParsePathGoEmitsDeadCodeRegistrationRoots`, `go/internal/reducer/handles_route_intents_test.go` | Shared reducer route projection proof | Exact standard-library registrations emit route entries; ambiguous wrappers, unknown mux receivers, and nonliteral patterns do not fabricate handlers. |
| Third-party router route truth | `third-party-router-route-truth` | supported | `framework_semantics.{gin,echo,chi,fiber}.route_entries` | `method, path, handler` for constructor-proven routers, literal route paths, literal group prefixes, and identifier handlers | `HANDLES_ROUTE` when reducer can resolve the exact handler | `go/internal/parser/go_dead_code_registrations_test.go::TestDefaultEngineParsePathGoEmitsThirdPartyRouteEntries`, `go/internal/reducer/handles_route_intents_test.go::TestBuildHandlesRouteIntentRowsEmitsGoFrameworkRouteMatches`, `go/internal/query/content_reader_framework_routes_test.go::TestParseFrameworkSemanticsExtractsGoFrameworkRoutes` | Parser-to-reducer-to-query route-entry proof | Gin, Echo, Chi, and Fiber exact route registrations emit route entries only when the router receiver is proven by a known constructor or literal group and the handler is an identifier. Dynamic paths, unknown receivers, method values, adapter wrappers, middleware chains, closures, generated routers, and runtime registrations remain unclaimed. |
| Outbound contracts | `outbound-contracts` | partial | - | - | - | Support-maturity guardrails | Explicit unsupported-contract wording on this page | HTTP/gRPC/topic client calls do not create deterministic cross-repo outbound contract edges today. |

## Framework And Library Support

Supported today:

- Standard-library HTTP registrations and signatures are modeled as derived
  roots.
- Exact `net/http` route registrations emit `framework_semantics.net_http`
  route entries for literal patterns and identifier handlers. Go 1.22
  `METHOD /path` patterns preserve the method; legacy patterns use `ANY`.
  `HANDLES_ROUTE` is projected only when the reducer resolves the exact handler.
- Exact Gin, Echo, Chi, and Fiber route registrations emit
  `framework_semantics.{gin,echo,chi,fiber}.route_entries` when a router variable
  is proven by `gin.New`/`gin.Default`, `echo.New`, `chi.NewRouter`, or
  `fiber.New`; literal `Group` prefixes are joined into the emitted path for
  Gin, Echo, and Fiber. Handlers must be identifier symbols.
- Cobra command registrations and signatures are modeled as derived roots.
- controller-runtime `Reconcile`, exported package API outside `cmd`,
  `internal`, and `vendor`, interface implementations, function values,
  generic constraints, type references, and dependency-injection callbacks are
  modeled as derived roots.

Not claimed today:

- Generated routers, middleware chains, dynamic or nonliteral route patterns,
  unknown receivers, method values, adapter functions, closures, webhook and
  worker registrations, reflection, build tags, plugin behavior, and broad
  public API surfaces remain exactness blockers.
- HTTP, gRPC, topic, and generated-client outbound contract extraction is not
  emitted as deterministic cross-repo contract truth today.

## Known Limitations
- Generic type constraints may not be fully captured
- Channel types not separately tracked
