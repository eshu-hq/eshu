# Scala Parser

This page tracks the checked-in Go parser contract in the current repository state.
Canonical implementation: `go/internal/parser/registry.go` plus the entrypoint and tests listed below.

## Parser Contract
- Language: `scala`
- Family: `language`
- Parser: `DefaultEngine (scala)`
- Entrypoint: `go/internal/parser/scala_language.go`
- Fixture repo: `tests/fixtures/ecosystems/scala_comprehensive/`
- Unit test suite: `go/internal/parser/engine_managed_oo_test.go`
- Integration validation: compose-backed fixture verification (see
  `../reference/local-testing.md`) plus the offline real-repo dogfood check
  (`scripts/dogfood-scala.sh`, see [Known Limitations](#known-limitations))

## Capability Checklist
| Capability | ID | Status | Extracted Bucket/Key | Required Fields | Graph Surface | Unit Coverage | Integration Coverage | Rationale |
|-----------|----|--------|------------------------|-----------------|---------------|---------------|----------------------|-----------|
| Functions (`def`) | `functions-def` | supported | `functions` | `name, line_number` | `node:Function` | `go/internal/parser/engine_managed_oo_test.go::TestDefaultEngineParsePathScala` | Compose-backed fixture verification | - |
| Classes | `classes` | supported | `classes` | `name, line_number` | `node:Class` | `go/internal/parser/engine_managed_oo_test.go::TestDefaultEngineParsePathScala` | Compose-backed fixture verification | - |
| Objects (`object`) | `objects-object` | supported | `classes` | `name, line_number` | `node:Class` | `go/internal/parser/engine_managed_oo_test.go::TestDefaultEngineParsePathScala` | Compose-backed fixture verification | - |
| Traits | `traits` | supported | `traits` | `name, line_number` | `node:Trait` | `go/internal/parser/engine_managed_oo_test.go::TestDefaultEngineParsePathScala` | Compose-backed fixture verification | - |
| Imports | `imports` | supported | `imports` | `name, line_number` | `relationship:IMPORTS` | `go/internal/parser/engine_managed_oo_test.go::TestDefaultEngineParsePathScala` | Compose-backed fixture verification | - |
| Function calls | `function-calls` | supported | `function_calls` | `name, line_number` | `relationship:CALLS` | `go/internal/parser/engine_managed_oo_test.go::TestDefaultEngineParsePathScala` | Compose-backed fixture verification | - |
| Generic function calls | `generic-function-calls` | supported | `function_calls` | `name, line_number` | `relationship:CALLS` | `go/internal/parser/engine_managed_oo_test.go::TestDefaultEngineParsePathScala` | Compose-backed fixture verification | - |
| Val definitions | `val-definitions` | supported | `variables` | `name, line_number` | `node:Variable` | `go/internal/parser/engine_managed_oo_test.go::TestDefaultEngineParsePathScala` | Compose-backed fixture verification | - |
| Var definitions | `var-definitions` | supported | `variables` | `name, line_number` | `node:Variable` | `go/internal/parser/engine_managed_oo_test.go::TestDefaultEngineParsePathScala` | Compose-backed fixture verification | - |
| Parent context (class_context) | `parent-context-class-context` | supported | `functions` | `name, line_number, class_context` | `property:Function.class_context` | `go/internal/parser/engine_managed_oo_test.go::TestDefaultEngineParsePathScala` | Compose-backed fixture verification | - |
| Exact Play/http4s route entries | `play-http4s-literal-route-truth` | supported | `framework_semantics.{play,http4s}.route_entries` | `method, path, handler` | `relationship:HANDLES_ROUTE` when the reducer resolves one exact handler | `go/internal/parser/scala_route_entries_test.go::TestDefaultEngineParsePathScalaEmitsExactPlayRouteEntries`, `go/internal/parser/scala_route_entries_test.go::TestDefaultEngineParsePathScalaEmitsExactHttp4sRouteEntries`, `go/internal/reducer/handles_route_scala_test.go::TestBuildHandlesRouteIntentRowsEmitsScalaPlayRouteMatches`, `go/internal/query/content_reader_framework_routes_scala_test.go::TestParseFrameworkSemanticsExtractsScalaRoutes` | Golden corpus gate | Exact Play `conf/routes` and `.routes` rows plus literal http4s `HttpRoutes.of` cases emit route entries. Reducer projection stays exact-only and skips unresolved or ambiguous handlers. |
| Dead-code roots | `dead-code-roots` | supported | `dead_code_root_kinds` | parser metadata | `code_quality.dead_code` exclusion metadata | `go/internal/parser/scala_dead_code_roots_test.go` | Fixture-backed dead-code validation | Derived roots for main, `App`, traits, overrides, Play, Akka, JUnit, ScalaTest, and lifecycle callbacks |

## Known Limitations
- Implicit conversions and given/using clauses (Scala 3) are not separately tracked
- Pattern matching extractors are not modeled as function calls
- For-comprehension generators are not surfaced as variable bindings
- Dead-code support is `derived`, not exact. Macros, implicit/given resolution,
  dynamic dispatch, reflection, sbt source sets, Play route files, compiler
  plugin output, and broad public API surfaces remain named exactness blockers.
- Historical note (not current grade evidence): an Issue #105 dogfood run
  covered `playframework/playframework` at
  `bcdc682de2250bbd0f2788bc5acc06f6d66ad5a7` and `scala/scala` at
  `25075e9b9b79954a0f99de515618901818822e62`, and both returned fresh `derived`
  dead-code API truth after queue drain. That run left no committed,
  offline-reproducible artifact, so it never backed a grade on its own.
- Scala's Real-Repo Validation grade is `real-repo-validated` (#5399), earned
  by a committed, offline-reproducible dogfood artifact: `scripts/dogfood-scala.sh`
  runs the standing `TestDogfoodScalaRealRepoSnapshot` regression test
  (`go/internal/parser/scala/dogfood_real_repo_test.go`) against the committed
  app-shaped corpus at `tests/fixtures/dogfood/scala_real_repo` (a synthetic
  Play-style `app/{controllers,models,services}` layout whose shape is
  informed by the same `playframework/playframework` and `scala/scala` pinned
  SHAs cited above, recorded as provenance metadata only and never fetched)
  and diffs the parser's bucket counts against the checked-in snapshot at
  `go/internal/parser/scala/testdata/dogfood_real_repo_snapshot.txt`. The
  script requires no network access or Docker. End-to-End Indexing stays
  `fixture-backed`: the corpus is not staged in `corpus_fixtures` in
  `scripts/verify-golden-corpus-gate.sh` and has no B-12 attribution, so it
  does not clear the `supported` bar (see
  [Parser Support Matrix](support-maturity.md#grade-definitions)). The
  evidence cells above cite the checked-in fixtures and tests.

## Framework And Library Support

Supported today:

- Play controller actions, Akka actor `receive`, JUnit methods, ScalaTest
  suites, and lifecycle callbacks are modeled as derived roots.
- Play `conf/routes` and `.routes` files emit exact route entries only for
  literal HTTP rows whose path is a deterministic Play path pattern and whose
  handler is a direct `controllers.Controller.method` target. The reducer only
  projects `HANDLES_ROUTE` when that handler resolves to one indexed function.
- http4s source emits exact route entries only for literal `HttpRoutes.of`
  cases shaped like `METHOD -> Root / "segment"` with a direct named handler
  identifier in the case body. Extractor routes, dynamic roots, nested response
  wrappers, and anonymous handler bodies are skipped.
- `main`, objects extending `App`, traits, same-file trait implementations,
  and overrides are also modeled as root evidence.
- Maven/Gradle vulnerability reachability can use Scala imports, calls, and
  SCIP evidence only when resolver evidence proves the dependency's package API
  prefix; this remains a prioritization signal and never emits `not_called`.

Not claimed today:

- Dynamic Play routes, namespaced Play controller targets, reverse routers,
  generated route files, broader http4s extractor/dsl shapes, macros, implicit
  and given/using resolution, compiler plugin output, sbt source sets, dynamic
  dispatch, reflection, and broad public API surfaces remain exactness
  blockers.
- Exact route-to-handler truth for Scala frameworks beyond the literal Play and
  http4s subset is not claimed.

## Parser Performance

The Scala parser collapses http4s route detection from a dedicated full-tree
tree-sitter walk into the main payload walk. The http4s check reads
`call_expression` nodes the main walk already visits and does not depend on
any state the main walk collects later, so it now collects candidate route
evidence unconditionally during the main pass; the import-bucket gate that
decides whether http4s is actually imported is applied afterward to the
routes already gathered, instead of re-walking the tree to collect them. This
lowers the common-case full-tree walk count for http4s files from 3 to 2
(`scalaCollectTypeContracts` stays a separate pre-pass: it seeds
`traitMethods`/`typeTraits` the main walk's dead-code-root classification
reads, so it must still run first) while keeping parser output
byte-identical, verified by a one-time old-vs-new `0/0` symmetric-diff over
the fixture corpus via the opt-in `SCALA_PARSE_DUMP` harness
(`equivalence_dump_test.go`, a manual differential — not a standing CI gate);
standing regression protection comes from the Scala parser package tests and
the B-12 golden snapshot (epic #4831, #4841). Contributors adding a new
framework-route detector should extend the shared pass rather than add
another full-tree walk when the detector has no dependency on another
detector's completed output.
