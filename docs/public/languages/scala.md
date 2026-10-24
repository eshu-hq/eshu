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
| Exact Play/http4s route entries | `play-http4s-literal-route-truth` | supported | `framework_semantics.{play,http4s}.route_entries` | `method, path, handler` | `relationship:HANDLES_ROUTE` when the reducer resolves one exact handler | `go/internal/parser/scala/scala_route_entries_test.go::TestDefaultEngineParsePathScalaEmitsExactPlayRouteEntries`, `go/internal/parser/scala/scala_route_entries_test.go::TestDefaultEngineParsePathScalaEmitsExactHttp4sRouteEntries`, `go/internal/reducer/code/call/materialization/routes_scala_test.go::TestBuildHandlesRouteIntentRowsEmitsScalaPlayRouteMatches`, `go/internal/query/content_reader_framework_routes_scala_test.go::TestParseFrameworkSemanticsExtractsScalaRoutes` | Golden corpus gate | Exact Play `conf/routes` and `.routes` rows plus literal http4s `HttpRoutes.of` cases emit route entries. Reducer projection stays exact-only and skips unresolved or ambiguous handlers. |
| Dead-code roots | `dead-code-roots` | supported | `dead_code_root_kinds` | parser metadata | `code_quality.dead_code` exclusion metadata | `go/internal/parser/scala/scala_dead_code_roots_test.go` | Fixture-backed dead-code validation | Derived roots for main, `App`, traits, overrides, Play, Akka, JUnit, ScalaTest, and lifecycle callbacks |

## Build Manifest (`build.sbt`)

`build.sbt` is admitted at the structured level (`DefaultEngine (sbt)`,
`go/internal/parser/sbt/`). The parser reads literal `libraryDependencies`
coordinates (`+=`, `++=`, `:=`; `%`, `%%`, `%%%`; an optional configuration such
as `% Test`) and the literal `scalaVersion`, and emits them as dependency rows
in the same shape the Gradle and Maven parsers use, with `package_manager`
`sbt`. A `%%` row also carries `sbt_binary_artifact` (for example
`org.typelevel:cats-core_2.13`) when the file holds exactly one distinct
literal `scalaVersion` and no other `scalaVersion` assignment.
A `scalaVersion` literal is read only where a
`libraryDependencies` statement would resolve (a column-0 statement or a
project's `.settings(...)` argument), from `ThisBuild / scalaVersion` whose
axis starts such a statement, or from `scalaVersion in ThisBuild` there. A
literal anywhere else (`inThisBuild(...)`, a wrapper, a call argument, a block,
a settings `val` or `def`, a lambda, another axis such as `in Test`) is
counted in `sbt_state.unresolved_declaration_count`, never recorded, and
clears `scala_version` and the hint for the whole file: the parser does not
track which projects it reaches. In the places a literal is read, any other
`scalaVersion` statement (a non-literal value such as `sys.env("SV")`, `~=`,
`<<=`, `.set(...)`, `.transform(...)`, `.withRank(...) :=`, `.:=(...)`, also
behind `ThisBuild /`) is counted and clears them too, whatever its order. A
read (`scalaVersion.value`) changes nothing. Two distinct literals also leave `scala_version` empty, with
`scala_version_count` 2: sbt applies the last assignment of a scope, and the
parser does not decide that order across scopes.

A row is `resolved` only when its whole statement matches a closed grammar
(the full EBNF is in `go/internal/parser/sbt/README.md`): a bare
`libraryDependencies` key at the top level of the file (a line that starts at
column 0) or as an argument of a project's `.settings(...)`, then `+=` with one element or `++=`/`:=` with a
`Seq(...)`, `List(...)` or `Vector(...)` of elements. An element is
`"org" %% "name" % "1.0"` (or `%`, `%%%`; the revision may be a top-level string
`val`), at most one `% config`, and infix calls from a short allowlist of
ModuleID methods that change neither coordinates nor configurations.

| Form | Row |
|---|---|
| `libraryDependencies += "a" %% "b" % "1" % Test` | `test`, resolved |
| `++= Seq("a" % "b" % "1", "c" % "d" % "2" % "provided")` | `compile` and `provided`, resolved |
| `% "it,test"`, `% Runtime`, `% Optional` | `test`, `runtime`, `compile` (optional), resolved |
| infix `exclude("o", "n")`, `excludeAll(...)`, `classifier "c"`, `intransitive()`, `withSources()`, `withJavadoc()`, `cross CrossVersion.full`, `force()`, `changing()` | scope unchanged, resolved |
| `lazy val core = project.settings(libraryDependencies += ...)` | resolved |
| a non-literal revision (`Versions.cats`, `s"${v}"`) | `unresolved`; siblings stay resolved |
| `Test /`, `ThisBuild /`, `Global /`, `core /`, `in Test`, `.in(Test)` | partial |
| `inConfig(...)`, `inThisBuild(...)`, any `in<Name>(...)`, a brace block, a settings `val` | partial |
| `.map(_ % Test)`, `.flatMap`, `.distinct`, `.filterNot`, `++ Seq(...)`, a lambda | partial |
| `("a" % "b" % "1") % Test`, a dotted call, `withConfigurations`, `withRevision`, `withName`, an unknown call | partial |
| a repeated `% config`, `% ""`, `% Custom`, `% "e2e"`, a compiler-tool configuration | partial |
| `% Test` leading the next line in the bare file | partial (inside `(` it is read) |
| an indented line at depth zero: a Scala 3 `if`/`then`/`else`, `def` or `val` body, `match` case, or `for`/`while` `do` body | partial |
| `% "Test"`, `% "Compile"` (configuration strings are case sensitive) | partial; `% "test"` and `% Test` are `test` |
| a string holding a backslash, triple-quoted too | opaque: no literal coordinate |
| `.transform(...)`, `.set(...)`, `.remove1(...)`, `.removeN(...)` anywhere (a `val` body, after an operator, an `if` branch), sbt 0.13's `<<=` | supersedes earlier rows, like `~=`; outside a grammar context, every row |
| `.withRank(...)` then `:=`, `~=`, `-=`, `--=`, `<<=` | supersedes earlier rows |
| `.withRank(...)` then `+=`, `++=`, `<+=`, `<++=` | partial; earlier rows unchanged |
| `.append1(...)`, `.appendN(...)`, `<+=`, `<++=`, any other assignment operator | partial; earlier rows unchanged |
| `.map(...)`, `.filter(...)` or any other member that returns no Setting and is no read | denied: supersedes every row, counted once |
| any other `libraryDependencies` occurrence: `(libraryDependencies) := Nil` at any depth, `(Compile / libraryDependencies) := Nil`, `libraryDependencies in Compile in Test := Nil`, `libraryDependencies set (x, pos)`, `libraryDependencies withRank r := Nil`, a bare key | denied: supersedes every row, counted |
| a read: `.value`, `.in(Test).value`, `(libraryDependencies in Test).value` | not a declaration, not counted |

A partial row keeps the coordinates read literally, carries a placeholder
`compile` scope, and is counted in `sbt_state.unresolved_declaration_count`; it
is never a resolved `compile`. sbt accepts one configuration per module: `%`
requires a module with no configuration yet, and `% ""` fails sbt's non-empty
check, so a repeated or empty configuration is a load error, not a replacement.
A list resolves to `compile` when any part is `compile`, to `runtime` when it
has `runtime` and no `compile` (a runtime dependency is also on the test
classpath), to `provided` for `provided` with `test`, and otherwise to the one
scope its parts share. `:=`, `~=`, `-=` and `--=` supersede the earlier rows of
the same project (a bare-file one supersedes every project row, and one in a
settings `val` or wrapper supersedes every row), which become partial.

sbt build files are Scala, so the parser never evaluates them. Versions held in
top-level string vals resolve in the same file; a val whose literal continues
on the next line (`.concat(...)`, `+ "x"`) does not. Ivy dynamic revisions and
ranges (`1.+`, `latest.release`, `[1.0,2.0)`, `]1.0,2.0]`) are partial.
Variables, `.value` settings, interpolated strings, helper objects, and
conditional or block-bodied collections are reported as `unresolved` or as
partial rows counted in `sbt_state.unresolved_declaration_count`.
`plugins.sbt`, the other `project/*.sbt` files, and `Build.scala` are not read.
The registry binds `build.sbt` by its base name, so `project/build.sbt`, sbt's
meta-build, is parsed: its `sbt_state.meta_build` is `true` and every row from
it is partial, because those dependencies are on the build definition's
classpath, not the project's. Package-registry correlation for sbt rows is not
wired yet: `sbt` is not a recognised package-manager alias, so these rows do not
join registry identities until [#8961](https://github.com/eshu-hq/eshu/issues/8961)
lands, and they keep the line-keyed entity id
([#8962](https://github.com/eshu-hq/eshu/issues/8962)).

Known limits of the sbt reading:

- `sbt_binary_artifact` is a file-wide hint, not sbt's resolved value. A single
  literal `scalaVersion` inside one project's settings also drives the `%%`
  rows of every other project in the file, `crossScalaVersions` is ignored, a
  suffixed version such as `2.13.0-M5` gets no hint, and a Scala before 2.10
  gets no hint because sbt cross-built those with the full version.
- sbt 2.x reads `build.sbt` as Scala 3, where a braceless block is an indented
  region at bracket depth zero. Only column-0 lines are read as top-level
  statements or version `val`s, so a top-level statement written indented, or
  with a comment before the key on its line, is partial. A closer that ends a
  bracket opened on an indented line keeps the rest of its line indented, so
  `}; libraryDependencies += ...` after it is partial too. A braceless project
  whose `.settings(` list continues on indented lines still resolves.
- A `val` declared inside a block is not tracked; a reference that shares its
  name with a top-level `val` resolves to the top-level value.
- A settings value from outside the file (`.settings(Common.settings)`) applied
  after a row can supersede it unseen, and so can an extension method defined
  outside the file that returns a Setting: only the members sbt's SettingKey
  API defines are judged.
- `scalaVersion` inside `inThisBuild(...)`, a settings `val` or `def`, or any
  other nested position is not read and clears `scala_version`, so a build
  that sets it there gets no `sbt_binary_artifact` hint, even with a top-level
  literal beside it. Both keys are default-deny: any occurrence that is no
  recognised statement and no read (`.value`, `.in(Test).value`,
  `(scalaVersion in Test).value`) clears `scala_version` or supersedes every
  `libraryDependencies` row, so a build that only reads a key through
  another member (`libraryDependencies.map(f)`) or shadows it loses rows it
  could have kept.
- `scala_version` fails closed: a `ThisBuild` literal plus a different
  project override, or two literals in one scope, report no version, even
  where sbt's last assignment would name one.
- `meta_build` is decided from the absolute path, so a repository checked out
  in a directory named `project` has its root `build.sbt` flagged and every row
  partial ([#8997](https://github.com/eshu-hq/eshu/issues/8997)).
- `ThisBuild /` and `Global /` keys are partial even where sbt delegation
  would read them, because the grammar keeps only the forms it can prove.

## Known Limitations
- Implicit conversions and given/using clauses (Scala 3) are not separately tracked
- Pattern matching extractors are not modeled as function calls
- For-comprehension generators are not surfaced as variable bindings
- Dead-code support is `derived`, not exact. Macros, implicit/given resolution,
  dynamic dispatch, reflection, sbt source sets, dynamic or generated Play
  routes, compiler plugin output, and broad public API surfaces remain named
  exactness blockers.
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
