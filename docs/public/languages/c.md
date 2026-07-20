# C Parser

This page tracks the checked-in Go parser contract in the current repository state.
Canonical implementation: `go/internal/parser/registry.go` plus the entrypoint and tests listed below.

## Parser Contract
- Language: `c`
- Family: `language`
- Parser: `DefaultEngine (c)`
- Entrypoint: `go/internal/parser/c_language.go`
- Unit test suite: `go/internal/parser/engine_systems_test.go`
- Integration validation: compose-backed fixture verification (see `../reference/local-testing.md`)

## Capability Checklist
| Capability | ID | Status | Extracted Bucket/Key | Required Fields | Graph Surface | Unit Coverage | Integration Coverage | Rationale |
|-----------|----|--------|------------------------|-----------------|---------------|---------------|----------------------|-----------|
| Functions | `functions` | supported | `functions` | `name, line_number` | `node:Function` | `go/internal/parser/engine_systems_test.go::TestDefaultEngineParsePathC` | Compose-backed fixture verification | - |
| Pointer-returning functions | `pointer-returning-functions` | supported | `functions` | `name, line_number` | `node:Function` | `go/internal/parser/engine_systems_test.go::TestDefaultEngineParsePathC` | Compose-backed fixture verification | - |
| Structs | `structs` | supported | `classes` | `name, line_number` | `node:Class` | `go/internal/parser/engine_systems_test.go::TestDefaultEngineParsePathC` | Compose-backed fixture verification | - |
| Unions | `unions` | supported | `classes` | `name, line_number` | `node:Class` | `go/internal/parser/engine_systems_test.go::TestDefaultEngineParsePathCTypedefAliases` | Compose-backed fixture verification | - |
| Enums | `enums` | supported | `classes` | `name, line_number` | `node:Class` | `go/internal/parser/engine_systems_test.go::TestDefaultEngineParsePathC` | Compose-backed fixture verification | - |
| Typedefs | `typedefs` | supported | `typedefs` | `name, line_number` | `node:Typedef + graph-first code/language-query + entity-resolve/context + semantic_summary, with content fallback` | `go/internal/parser/engine_systems_test.go::TestDefaultEngineParsePathCTypedefAliasEmitsDedicatedEntities`, `go/internal/projector/runtime_test.go::TestRuntimeProjectEnqueuesSemanticEntityMaterializationForAnnotationTypedefTypeAliasComponentAndFunction`, `go/internal/reducer/semantic_entity_materialization_test.go::TestSemanticEntityMaterializationHandlerWritesAndRetracts`, `go/internal/storage/cypher/semantic_entity_test.go::TestSemanticEntityWriterWritesAnnotationTypedefTypeAliasComponentAndFunctionNodes`, `go/internal/query/language_query_graph_first_test.go::TestHandleLanguageQuery_CTypedefPrefersGraphPathAndEnrichesMetadata`, `go/internal/query/language_query_graph_first_test.go::TestHandleLanguageQuery_CTypedefUsesGraphMetadataWithoutContent`, `go/internal/query/entity_summary_test.go::TestBuildEntitySemanticSummaryTypedef`, `go/internal/query/entity_content_c_fallback_test.go::TestResolveEntityFallsBackToContentTypedefEntity`, `go/internal/query/entity_content_c_fallback_test.go::TestGetEntityContextFallsBackToContentTypedefEntity` | Compose-backed fixture verification | The Go parser emits dedicated typedef entities, the projector/reducer/Cypher graph path now persists them as first-class `Typedef` graph nodes, the normal Go language-query path prefers graph rows before falling back to content, and entity-resolve/context surfaces return them with semantic summaries. |
| Includes | `includes` | supported | `imports` | `name, line_number` | `relationship:IMPORTS` | `go/internal/parser/engine_systems_test.go::TestDefaultEngineParsePathC` | Compose-backed fixture verification | - |
| Function calls | `function-calls` | supported | `function_calls` | `name, line_number` | `relationship:CALLS` | `go/internal/parser/engine_systems_test.go::TestDefaultEngineParsePathC` | Compose-backed fixture verification | - |
| Dead-code roots | `dead-code-roots` | supported | `functions.dead_code_root_kinds` | `c.main_function, c.public_header_api, c.signal_handler, c.callback_argument_target, c.function_pointer_target` | `code_quality.dead_code` derived root suppression | `go/internal/parser/c_dead_code_roots_test.go::TestDefaultEngineParsePathCDeadCodeFixtureExpectedRoots`, `go/internal/parser/c_dead_code_roots_test.go::TestDefaultEngineParsePathCMarksOnlyIncludedHeaderPrototypesAsPublicAPI`, `go/internal/parser/c_dead_code_roots_test.go::TestDefaultEngineParsePathCMarksCallbackArgumentTargets`, `go/internal/query/code_dead_code_c_roots_test.go::TestHandleDeadCodeExcludesCRootKindsFromMetadata` | Compose-backed fixture verification | C dead-code maturity is `derived`. Header public API roots are bounded to local headers directly included by the source file, so parser cost stays per-file and broad non-static functions are not automatically protected. |
| Variables (initialized declarations) | `variables-initialized-declarations` | supported | `variables` | `name, line_number` | `node:Variable` | `go/internal/parser/engine_systems_test.go::TestDefaultEngineParsePathC` | Compose-backed fixture verification | - |
| Macros (`#define`) | `macros-define` | supported | `macros` | `name, line_number` | `node:Macro` | `go/internal/parser/engine_systems_test.go::TestDefaultEngineParsePathC` | Compose-backed fixture verification | - |

## Framework And Library Support

Supported today:

- This parser does not claim framework-level support.
- Derived root evidence includes `main`, directly included local header
  declarations, signal handlers, callback arguments, and direct
  function-pointer initializer targets.
- C HTTP callback registries are not exact route entries today; C does not emit
  `framework_semantics.*.route_entries` or `HANDLES_ROUTE` edges.

Not claimed today:

- Transitive include graphs, build-target selection, callback registries,
  dynamic symbol lookup, and external linkage remain exactness blockers.
- Exact route-to-handler truth for C HTTP frameworks or project-local callback
  registries is tracked by
  [#4116](https://github.com/eshu-hq/eshu/issues/4116).

## Known Limitations
- Function pointer declarations are not modeled as callable entities
- Preprocessor macros with complex expansions are captured by name only
- Transitive include graphs and build-target-specific conditional compilation
  are exactness blockers for dead-code cleanup
- Dynamic symbol lookup and broad callback registry semantics remain non-exact
- Variadic functions do not expose their variadic argument types

## Dead-Code-Roots Walk Merge (issue #4870)

`Parse` previously ran two full-tree tree-sitter walks per file: the main
payload walk, then a second `shared.WalkNamed` traversal inside
`annotateCDeadCodeRoots` to resolve `call_expression` and `declaration` nodes
against `payload["functions"]` (signal-handler registrations, callback
arguments, and function-pointer initializer targets). The second walk visited
node kinds the main walk already visits, and depended on nothing except
`payload["functions"]`, which the main walk has already fully populated by the
time annotation runs.

`annotateCDeadCodeRoots` no longer runs a second full-tree traversal. Instead,
resolution-candidate node pointers (`call_expression`, `declaration`) are
gathered via `shared.CloneNode` during `Parse`'s main payload walk, in a single
ordered (pre-order) slice, and resolved in one in-memory loop after
`payload["functions"]` is fully populated. Preserving one interleaved gather
slice (not per-kind grouped slices) matters: when a single function
accumulates root kinds from both a call expression and a declaration, the
`dead_code_root_kinds` slice order must match their relative source position,
exactly as the eliminated second walk produced (mirrors the ordering fix
landed for C++ in #4844/#4924). Forward references (a call or declaration
naming a function defined later in the file) still resolve correctly, because
the function map is complete before any resolution loop runs.

- **Performance Evidence:** `BenchmarkParse/c` on the ~10K-LOC `c_regression`
  fixture (Apple M1 Max, `-count=10`): B/op went from ~26.51Mi to ~23.07Mi
  (-12.99%, `benchstat` p=0.000) and allocs/op from ~931.6k to ~795.8k
  (-14.58%, `benchstat` p=0.000). `sec/op` moved from 1.630s to 1.590s but was
  not statistically significant at this sample size (`benchstat` p=0.631,
  ±40-47% noise from concurrent local load); the allocation and B/op deltas
  are the reliable signal for this refactor. A throwaway isolated-annotation-step
  microbenchmark (300-repeat synthetic fixture, ~300 function definitions/~600
  call expressions/~300 declarations) showed the gather-then-resolve candidate
  at roughly 10x fewer ns/op and B/op than the two-walk baseline, confirming
  the mechanism before the change was wired into `Parse`. Equivalence: `0/0`
  symmetric diff (`diff` + `comm -3` both empty) over the 24-file C/H fixture
  corpus via the `C_PARSE_DUMP` harness (`equivalence_dump_test.go`, a manual
  differential, not a standing CI gate), and the standing
  `TestParseFullTreeWalkCount` regression pins the walk count at 7 (down from
  8 pre-change: the main walk plus bounded `firstNamedDescendant` subtree
  scans, with the second full-tree dead-code-roots walk eliminated).
- **No-Observability-Change:** this package emits no telemetry by design; the
  gather-then-resolve refactor neither adds nor removes spans, metrics, or
  logs. Operators still diagnose C parsing through the existing collector
  parse-stage logs and `eshu_dp_file_parse_duration_seconds`.
