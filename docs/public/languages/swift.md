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
| Vapor literal route entries | `vapor-literal-route-entries` | supported | `framework_semantics.vapor.route_entries` | `method, path, handler` | `relationship:HANDLES_ROUTE` | `go/internal/parser/engine_swift_vapor_routes_test.go::TestDefaultEngineParsePathSwiftVaporRouteEntries`, `go/internal/parser/engine_swift_vapor_routes_test.go::TestDefaultEngineParsePathSwiftVaporRouteEntriesLiteralGroups`, `go/internal/reducer/handles_route_swift_test.go::TestBuildHandlesRouteIntentRowsEmitsSwiftVaporRouteMatches` | Focused reducer projection validation | Exact only in files importing `Vapor`, for literal registrations on receivers typed `Application` or `RoutesBuilder`, including literal route groups whose receiver is already proven and whose closure parameter is a simple identifier. Route handlers must use a simple `use:` handler identifier. |
| Dead-code roots | `dead-code-derived-roots` | derived | `dead_code_root_kinds` | parser metadata | `code_quality.dead_code` root suppression | `go/internal/parser/swift_dead_code_roots_test.go::TestDefaultEngineParsePathSwiftEmitsDeadCodeRootKinds`, `go/internal/parser/swift_dead_code_roots_test.go::TestDefaultEngineParsePathSwiftRequiresVaporImportForRouteHandlerRoot`, `go/internal/query/code_dead_code_swift_roots_test.go::TestHandleDeadCodeExcludesSwiftRootKindsFromMetadata` | Fixture-backed dead-code validation | Parser metadata marks `@main` types, top-level `main`, SwiftUI `App` types and `body`, protocol methods and same-file implementations, constructors, overrides, UIKit application delegate callbacks, Vapor route handlers, XCTest methods, and Swift Testing `@Test` functions as non-exact roots. Vapor route-handler roots now require the file to `import Vapor` (mirroring the exact Vapor route-entries gate): a `use:` handler in a file that does not import Vapor no longer roots. |

## Known Limitations
- Property wrappers are not tracked as distinct decorators
- `@objc` and dynamic dispatch attributes are not modeled in the graph
- Computed property bodies are not traversed for embedded function calls
- SwiftPM `Package.resolved` dependency evidence is handled by the JSON
  exact-name parser, not the `.swift` source parser. Only remote
  source-control pins with an exact `state.version` become dependency rows;
  `Package.swift` manifests, branch-only pins, revision-only pins, local pins,
  path dependencies, and SwiftPM target resolution remain unsupported for
  supply-chain impact evidence.
- Dead-code cleanup remains non-exact. Macro expansion, conditional
  compilation, SwiftPM target membership, protocol witness resolution, dynamic
  dispatch, property-wrapper generated code, result-builder expansion,
  Objective-C runtime dispatch, and broad public API surfaces are named
  exactness blockers rather than hidden assumptions.

## Framework And Library Support

Supported today:

- SwiftUI app types and `body`, UIKit application delegate callbacks, Vapor
  route handlers, XCTest methods, and Swift Testing `@Test` functions are
  modeled as derived roots. Vapor route-handler roots require the file to
  `import Vapor` (the same gate as exact Vapor route entries); a `use:`
  handler in a non-Vapor file is not rooted.
- Files importing `Vapor` can emit exact route entries for receiver route
  registrations on receivers typed `Application` or `RoutesBuilder`, with
  literal path segments and a simple `use:` handler identifier. Literal route
  groups are included when the parent receiver is already proven, the prefix
  segments are literal, and the closure parameter is a simple identifier. These
  entries are exposed through `framework_semantics.vapor.route_entries` and can
  materialize `HANDLES_ROUTE` edges when the handler resolves uniquely.
- `@main` types, top-level `main`, protocol methods and same-file
  implementations, constructors, and overrides are modeled as root evidence.

Not claimed today:

- Macros, conditional compilation, SwiftPM target resolution, protocol witness
  resolution, property-wrapper generated code, result builders, Objective-C
  runtime dispatch, and broad public API surfaces remain exactness blockers.
- Vapor closure handlers, nonliteral path expressions, nonliteral or computed
  route groups, generated routes, and other Swift web frameworks remain
  unsupported for exact route-to-handler truth.

## Parser Performance

`collectSwiftSemanticFacts` folds three independent full-tree passes — the
Vapor import check, the Vapor route-receiver scan, and the `use:` handler
scan over `value_argument` nodes — into the same manual-recursion walk that
already collects type conformances and protocol methods
(`collectSwiftFileFacts`). None of those three checks depends on another's
output while collecting, so they now run as extra cases in the existing
per-node dispatch instead of three additional `shared.WalkNamed` traversals.
Only the Vapor-gated route-entries pass (`swiftVaporRouteEntries`) still runs
as its own traversal afterward, since it genuinely depends on the
now-complete route-receiver map to resolve nested `.group(...)` prefixes; the
main `swiftExtractor.walk` payload pass is unaffected. This keeps parser
output byte-identical, verified by a one-time old-vs-new `0/0` symmetric-diff
over the fixture corpus via the opt-in `SWIFT_PARSE_DUMP` harness
(`equivalence_dump_test.go`, a manual differential — not a standing CI gate);
standing regression protection comes from the Swift parser package tests and
the B-12 golden snapshot (epic #4831, #4841). Contributors adding new
same-file semantic evidence should extend `collectSwiftFileFacts` rather than
add another full-tree walk when the evidence has no dependency on another
collector's completed output.
