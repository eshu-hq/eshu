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
| Literal HTTP framework route truth | `cpp-literal-http-framework-route-truth` | supported | `framework_semantics.{crow,drogon,pistache}.route_entries` | `method, path, handler` for literal route registrations with named handlers | `HANDLES_ROUTE` when reducer can resolve the exact handler | `go/internal/parser/engine_cpp_route_semantics_test.go`, `go/internal/reducer/handles_route_cpp_test.go`, `go/internal/query/content_reader_framework_routes_test.go` | Parser-to-reducer-to-query route-entry proof | Literal Crow, Drogon, and Pistache registrations emit exact route entries only when source proves the HTTP method, route path, and handler target. |
| Dead-code roots | `dead-code-roots` | derived | `functions.metadata.dead_code_root_kinds` | `cpp.main_function, cpp.public_header_api, cpp.virtual_method, cpp.override_method, cpp.callback_argument_target, cpp.function_pointer_target, cpp.node_addon_entrypoint` | `code_quality.dead_code` root suppression | `go/internal/parser/cpp_dead_code_roots_test.go` | Compose-backed C++ dogfood verification | Entry points, directly included local header declarations, virtual and override methods, direct callback arguments, direct function-pointer initializer targets, and Node native-addon entrypoints are modeled as non-exact roots. |

## Framework And Library Support

Supported today:

- Node native-addon entrypoints are modeled as derived roots.
- Derived root evidence also includes `main`, directly included local header
  declarations, virtual and override methods, callback arguments, and direct
  function-pointer initializer targets.
- Literal Crow, Drogon, and Pistache route declarations emit exact
  `framework_semantics.{crow,drogon,pistache}.route_entries` when the parser can
  prove the HTTP method, literal path, and named handler target in source.
  `HANDLES_ROUTE` is projected only when the reducer resolves that handler to
  one Function entity.

Not claimed today:

- Macro expansion, conditional compilation, build targets, template
  instantiation, overload resolution, broad virtual dispatch, callback
  registries, generated routes, dynamic route paths or methods, lambda/inline
  handlers, runtime plugin loading, and dynamic symbol lookup remain exactness
  blockers.
- C++ project-local callback registries remain source/dead-code/root evidence
  only and do not emit route-to-handler truth unless they match the literal
  framework shapes above.

## Known Limitations
- Template specializations are not separately modeled
- Operator overloads are captured as regular functions without operator context
- Preprocessor-conditional code blocks are parsed as-is without branch tracking
- Dead-code remains non-exact until macro expansion, conditional compilation,
  build-target selection, transitive include graphs, template instantiation,
  overload resolution, virtual dispatch breadth, callback registration, dynamic
  symbol lookup, and external linkage are modeled or scoped out.

## Parser Performance

The C++ parser collapses Crow/Drogon/Pistache framework-route detection from
a dedicated full-tree tree-sitter walk into Parse's main payload walk. The
route check reads `call_expression` nodes the main walk already visits (for
`appendCall`) and depends on nothing besides that node's own text, so
`cppRouteCollector.collect` now runs from the same case as
`buildCPPFrameworkSemantics`'s standalone pass did. This lowers the
framework-detection walk count while keeping parser output byte-identical,
verified by a one-time old-vs-new `0/0` symmetric-diff over the fixture
corpus via the opt-in `CPP_PARSE_DUMP` harness (`equivalence_dump_test.go`, a
manual differential — not a standing CI gate); standing regression
protection comes from the C++ parser package tests and the B-12 golden
snapshot (epic #4831, #4841). Contributors adding a new framework-route
detector should extend `cppRouteCollector` rather than add another full-tree
walk when the detector has no dependency on another collector's completed
output.

The dead-code-roots annotation (`annotateCPPDeadCodeRoots`) no longer runs a
second full-tree `shared.WalkNamed` traversal. Instead, resolution-candidate
node pointers (`function_definition`, `call_expression`, `declaration`) are
gathered via `shared.CloneNode` during Parse's main payload walk and resolved
in in-memory `for` loops after `payload["functions"]` is fully populated.
This eliminates one `shared.WalkNamed` call per parse (walk count 15→14 on the
walk-count fixture), verified by `walk_count_test.go`.

- **Performance Evidence:** `BenchmarkParse/cpp` on the ~10K-LOC
  `cpp_regression` fixture (Apple M5 Max, `-count=10`): median ns/op went
  from ~175M (merge-base 8085fd1b8) to ~145M (~17% faster), B/op from ~36.3M
  to ~32.9M (~9% fewer), allocs/op from ~1.13M to ~993K (~12% fewer). The
  isolated annotation-step theory microbenchmark (2044-line padded fixture,
  252 func_defs / 224 call_exprs / 56 decls) confirmed a ~2.06x speedup on
  the targeted walk. Equivalence: `0/0` symmetric-diff over the C++ fixture
  corpus via the `CPP_PARSE_DUMP` harness (70 files, diff + comm -3 empty).
- **No-Observability-Change:** this package emits no telemetry by design;
  the gather-then-resolve refactor neither adds nor removes spans, metrics, or
  logs. Operators still diagnose C++ parsing through the existing collector
  parse-stage logs and `eshu_dp_file_parse_duration_seconds`.
