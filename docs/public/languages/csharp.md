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
| Dead-code roots | `dead-code-roots` | derived | `functions.metadata.dead_code_root_kinds` | `name, line_number, dead_code_root_kinds` | `code_quality.dead_code` root suppression | `go/internal/parser/csharp_dead_code_roots_test.go::TestDefaultEngineParsePathCSharpEmitsDeadCodeRootKinds`, `go/internal/query/code_dead_code_csharp_roots_test.go::TestHandleDeadCodeExcludesCSharpRootKindsFromMetadata` | Compose-backed C# dogfood required by issue #97 | Main methods, constructors, overrides, same-file interface methods and implementations, ASP.NET controller actions, hosted-service callbacks, test methods, and serialization callbacks are modeled as derived roots. |
| ASP.NET attribute route truth | `aspnet-attribute-route-truth` | supported | `framework_semantics.aspnet.route_entries` | `method, path, handler` for literal controller/action attributes; handlers are class-qualified when the declaring controller is known | `HANDLES_ROUTE` when reducer can resolve the exact handler | `go/internal/parser/csharp_route_semantics_test.go::TestDefaultEngineParsePathCSharpASPNetAttributeRouteEntries`, `go/internal/reducer/handles_route_csharp_test.go::TestBuildHandlesRouteIntentRowsEmitsCSharpASPNetExactEntries` | Shared reducer route projection proof | Literal ASP.NET Core MVC/Web API attributes emit exact entries only when the route path and method handler are source-proven. Token substitution, nonliteral routes, `[NonAction]`, generated routes, and convention routing do not fabricate handler edges. |
| ASP.NET minimal API route truth | `aspnet-minimal-api-route-truth` | supported | `framework_semantics.aspnet_minimal_api.route_entries` | `method, path, handler` for literal `Map*` calls with named handlers | `HANDLES_ROUTE` when reducer can resolve the exact handler | `go/internal/parser/csharp_route_semantics_test.go::TestDefaultEngineParsePathCSharpASPNetMinimalAPIRouteEntries`, `go/internal/reducer/handles_route_csharp_test.go::TestBuildHandlesRouteIntentRowsEmitsCSharpASPNetExactEntries` | Shared reducer route projection proof | Literal `MapGet`, `MapPost`, `MapPut`, `MapPatch`, `MapDelete`, and literal `MapMethods` registrations emit exact entries only when the handler is a bare identifier. Lambdas, method values, dynamic paths, endpoint filters, and generated/runtime route registrations remain unclaimed. |
| ASP.NET convention route truth | `aspnet-convention-route-truth` | partial | - | - | - | `go/internal/parser/csharp_route_semantics_test.go` | Explicit unsupported-route wording on this page | Controller/action conventions, `[controller]`/`[action]` token expansion, runtime discovery, and generated manifests are not exact route-to-handler truth today. |
| ASP.NET endpoint-routing truth | `aspnet-endpoint-routing-route-truth` | partial | - | - | - | `go/internal/parser/csharp_route_semantics_test.go` | Explicit unsupported-route wording on this page | `MapControllerRoute`, `MapGroup`, endpoint filters, runtime discovery, and generated manifests are not exact route-to-handler truth today. |

## Framework And Library Support

Supported today:

- ASP.NET controller actions, hosted-service callbacks, test methods, and
  serialization callbacks are modeled as derived roots. Derived ASP.NET roots
  are separate from exact route truth and do not by themselves create
  `HANDLES_ROUTE` edges.
- Literal ASP.NET Core MVC/Web API attributes emit
  `framework_semantics.aspnet.route_entries` when the parser can prove the HTTP
  method, route path, and method handler from local source. Class-level
  `[Route("...")]` prefixes and method-level `HttpGet`/`HttpPost`/`HttpPut`/
  `HttpPatch`/`HttpDelete`/`HttpHead`/`HttpOptions` or literal method
  `[Route("...")]` attributes are in scope; `[NonAction]`, nonliteral paths,
  and `[controller]`/`[action]` token expansion are skipped.
- Literal ASP.NET Core minimal API registrations emit
  `framework_semantics.aspnet_minimal_api.route_entries` for `MapGet`,
  `MapPost`, `MapPut`, `MapPatch`, `MapDelete`, and literal `MapMethods`
  calls when the route path is a source literal and the handler argument is a
  bare identifier. `HANDLES_ROUTE` is projected only when the reducer resolves
  that handler to exactly one Function entity.
- Main methods, constructors, overrides, and same-file interface methods and
  implementations are also modeled as root evidence.
- `.csproj` PackageReference entries are parsed by the separate
  `nuget_project` parser path into repository dependency evidence. Requested
  versions, resolved MSBuild property versions, unresolved-property partial
  evidence, and PrivateAssets dev/test signals are preserved for the
  supply-chain impact reducer.

Not claimed today:

- Reflection, dependency injection, source generators, partial type merging,
  project references, dynamic dispatch, and broad public API surfaces remain
  exactness blockers for code reachability. NuGet project-reference package
  identity is skipped unless it is represented as PackageReference package
  evidence.
- ASP.NET controller/action conventions, tokenized `[controller]` and
  `[action]` routes, `MapControllerRoute`, `MapGroup`, endpoint filters, route
  values derived from dependency injection, reflection, generated manifests,
  implicit global usings, and runtime-discovered registrations are not emitted
  as exact route-to-handler truth today.

## Known Limitations
- Extension methods are not tagged as extensions in the graph
- Partial class merging across files is not performed
- Nullable reference types (`T?`) not surfaced as distinct type metadata
- Reflection, dependency injection, source generators, project references,
  partial type merging, dynamic dispatch, and broad public API surfaces remain
  exactness blockers for dead-code cleanup.
- C# route truth requires explicit local source evidence. Implicit/global using
  configuration and project-level endpoint conventions are not resolved.

## Parser Performance

The C# parser collapses its semantic-fact collection and framework-route
detection from separate full-tree tree-sitter walks into single passes:
`collectCSharpSemanticFacts` gathers candidate method-declaration nodes during
the one type-collection walk and resolves interface methods afterward (once the
declared-type counts are complete), and the ASP.NET attribute-route and
minimal-API route detectors, which inspect disjoint node kinds, share one walk.
This lowers the common-case full-tree walk count from 4 to 2 while keeping
parser output byte-identical (a `0/0` symmetric-diff over the fixture corpus
gates it; epic #4831, #4869). Contributors adding a new fact collector should
extend the shared pass rather than add another full-tree walk when the
collector has no dependency on another collector's completed output.
