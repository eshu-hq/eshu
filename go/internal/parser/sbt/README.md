# sbt Parser

## Purpose

This package owns repository-side `build.sbt` dependency parsing. It turns each
build definition into `content_entity`-shaped dependency rows the supply-chain
impact reducer reads, plus the literal Scala version. It exists because the
language ledger names `build.sbt` as the Scala manifest and the registry did
not admit it (#8938).

## Ownership boundary

The package owns `build.sbt` scanning only. The parent parser package owns
registry dispatch, file discovery, and the engine wrapper
(`go/internal/parser/gradle_language.go`, beside the Gradle wrapper). The reducer owns matching emitted rows
to package-registry identities.

## Exported surface

- `Parse(path string, isDependency bool, options shared.Options) (map[string]any, error)`
- `MaxDependencyRows`, `MaxFieldBytes`, `LanguageName`

The payload follows `shared.BasePayload(path, "sbt", isDependency)`. The
`variables` bucket holds one row per literal coordinate:

- `name`: `organization:name`, exactly as written. For `%%` and `%%%` this is
  the unsuffixed name, not the published artifact name.
- `value`: the literal revision, the revision a top-level string `val`
  resolves to, or the raw identifier or interpolation text when unresolved.
- `section`: `libraryDependencies`, with `:<Config>` appended when the
  declaration names a configuration.
- `config_kind`: always `dependency`; `package_manager` and `lang`: `sbt`.
- `dependency_scope`: `compile`, `test`, `provided`, or `runtime`, mapped as
  described under "Scope mapping". The raw spelling stays in
  `sbt_configuration`.
- `dependency_resolution_state`: `resolved`, `partial`, or `unresolved`, with
  `dependency_unresolved_keys` when unresolved.
- `direct_dependency`: `true`; `dependency_path_kind`: `manifest`;
  `dependency_optional`: `true` for an `Optional` configuration.
- `sbt_cross_version`: `none` (`%`), `binary` (`%%`), `scalajs` (`%%%`), or
  `custom` (a `cross` modifier).
- `sbt_binary_artifact`: `organization:name_<binary>` for a `%%` row when the
  file holds exactly one literal `scalaVersion`, so a consumer can match the
  published artifact. Absent otherwise.

`sbt_state` carries `state`, `scala_version` (the first `ThisBuild`-scoped
literal, else the first literal, else empty), `scala_version_count`,
`dependency_count`, `dependency_overflow_count`, and
`unresolved_declaration_count`.

## Scope mapping

The scope comes from an explicit `% config` operand, which may sit after the
coordinate (`"a" % "b" % "1" % Test`), on the next line as a leading infix
(`"a" % "b" % "1"` then `  % Test`, valid in sbt 2 and Scala 3), after a
parenthesised coordinate (`("a" %% "b" % "1").cross(...) % Test`), or on a
collection (`Seq(...).map(_ % Test)`, `Seq(...) map (_ % "test")`). Without one
the scope is `compile`.

### Default is undetermined

After a coordinate the scanner reads the tail with an allowlist, not a denylist.
Only these keep the scope readable:

| Tail | Why it is safe |
|---|---|
| `% config` | the configuration operand; the last one wins |
| `exclude`, `excludeAll` | add exclusion rules |
| `classifier`, `artifacts`, `from` | add or locate artifacts |
| `intransitive`, `notTransitive` | set the transitive flag |
| `withSources`, `withJavadoc` | add sources and javadoc artifacts |
| `cross`, `withCrossVersion` | set the cross-version rule (`sbt_cross_version` is `custom`) |
| `force`, `changing` | set the force and changing flags |

These are the ModuleID methods that change something other than the module's
configurations. Any other identifier, call, operator, literal, or bare group in
the tail leaves the scope undetermined: `withConfigurations`, `configure`, an
unknown `%?` operator, a user-defined extension method, a line that starts with
an infix operator other than `%`, a `(a, b)` tuple. The row is `partial` and
counted, never a resolved `compile`. The same rule applies to a `.map(...)`
body: it must be `_` followed by the shapes above (`_ % Test`,
`_.withSources()`). A lambda (`d => d.withSources()`), a block, or an unknown
call in the body is undetermined. After a collection only `toSeq`, `toList`,
`toVector`, `toSet`, `toArray`, `distinct`, `filter`, `filterNot`, and `map` keep
the scope; a `flatMap`, `collect`, `zip`, or fold does not.

### Where the key is declared

A plain `libraryDependencies`, `ThisBuild /`, `Global /`, `ThisProject /`,
`libraryDependencies in ThisBuild`, `inThisBuild(...)`, and `core /` for a
`core` the file declares as a project (`lazy val core = project`,
`(project in file(...))`, `crossProject(...)`) are the project-level key and
read as `compile` unless an operand says otherwise. Every other form is
undetermined, and an explicit `% Test` does not rescue it: `Test /`, `Compile /`,
`IntegrationTest /`, a custom configuration, a task axis, `libraryDependencies in
Test`, `libraryDependencies.in(Test)`, and anything inside `inConfig(...)`,
`inTask(...)`, `inScope(...)` or another `in<Name>(...)(...)` wrapper. sbt
resolves `update` from the project-level key, so a configuration-scoped key is
not evidence of a test dependency, and the scanner no longer reports one. A
project referenced before its `lazy val` is also undetermined: the scan is one
pass.

A configuration is a list split on `,` and `;`, and each part is judged by the
left side of a `->` mapping. `test`, `it`, `integrationtest`, and
`test-internal` mean `test`; `runtime` and `runtime-internal` mean `runtime`;
`provided` means `provided`; `compile`, `optional` and `compile-internal` mean
`compile`. A list resolves by this table:

| Parts | `dependency_scope` | Why |
|---|---|---|
| all one scope (`"it,test"`) | that scope | no disagreement |
| any `compile` part (`"compile;test"`) | `compile` | a compile dependency is on every classpath |
| `runtime` without `compile` (`"runtime,test"`, `"runtime->runtime;test->test"`) | `runtime` | runtime is also on the test classpath, so the dependency is not compile |
| `provided` with `test`, no compile or runtime | `provided` | provided is on the test classpath |
| `runtime` with `provided` | undetermined | compile-time only plus run-time only: not one scope |

The compiler-tool configurations (`plugin`, `scala-tool`, `scala-doc-tool`) and
the artifact-type configurations (`docs`, `sources`, `pom`) are not classpath
scopes, so they are undetermined, the same way Gradle gives `annotationProcessor`
and `kapt` their own scope instead of folding them into `compile`. `addCompilerPlugin`
expands to `"plugin->default(compile)"` and is therefore undetermined.

When a coordinate carries more than one `% config`, the last one wins, as in
sbt, where each `moduleID % config` replaces the configuration. Chained
`.map(_ % A).map(_ % B)` calls resolve the same way, as one replayed effect.

An empty configuration (`% ""`) or one made only of separators (`% ","`) names
no configuration and is undetermined, because sbt gives it no documented
meaning and the default `compile` would be a guess. The empty `.map(_ % "")` is
the same.

A scope the scanner cannot read is never a resolved `compile`. A custom
`Config` value, a variable such as `% testConfig`, an unrecognised string, a
configuration string over `MaxConfigBytes` (256), a
`.map(dep => dep % Test)` lambda, an unknown tail, or a scoped key keeps
`dependency_scope` `compile` as a placeholder, sets the row's
`dependency_resolution_state` to `partial`, and adds the row to
`sbt_state.unresolved_declaration_count`.

## Bounds

One forward pass, no recursion, one token of pushback: work and allocation are
linear in the input for a 100 KB single line, 200k nested parentheses, 100k
dependency lines, a 10 MB configuration list, or a 100 KB configuration applied
with `.map(_ % "...")` to 1,000 dependencies. A configuration is resolved once
per `% config` or per collection `.map`, never per dependency, and the result
(scope, optional flag, canonical text) is copied by value to each dependency.
`.map` calls after one collection are replayed into one effect and written to
its dependencies once, however many calls are chained. `dependency_scope` is
undetermined for a configuration string over `MaxConfigBytes` (256): it is not
split or stored, the row is `partial`, and `sbt_configuration` and `section`
carry `(overlong)` instead. Real configurations are a few bytes and the longest
common mapping is under 50. Rows cap at `MaxDependencyRows` (10,000); the excess is counted, not
stored. A coordinate field over `MaxFieldBytes` (512) is not a coordinate, and
an identifier chain over that length (a version reference or a configuration)
is consumed without being buffered: a version chain is counted as unresolved
and a configuration chain is recorded as `(overlong)` with an undetermined
scope, the same as an overlong configuration string. At most 1,024 top-level string vals and 64 distinct Scala versions are
tracked. A leading UTF-8 byte order mark is ignored.

## What this parser never does

- Evaluate Scala, run sbt, or read `project/*.sbt`, `plugins.sbt`,
  `Build.scala`, `Dependencies.scala`, or any sibling file.
- Resolve `.value` settings, interpolated strings, helper objects, or
  conditional (`if`) and block-bodied collections. These are counted in
  `unresolved_declaration_count`.
- Resolve a version through a `val` declared inside a block or after the
  reference's own statement is parsed; only top-level literal vals resolve, at
  the end of the file, so declaration order does not matter.
- Read a second `Seq(...)` joined by `++` as anything but another literal
  collection: a non-literal operand is counted in
  `unresolved_declaration_count`.
- Emit a row for `addSbtPlugin`, `dependsOn`, resolvers, or `crossScalaVersions`.

## Known limits

- `sbt_binary_artifact` is a hint derived from the file's single literal
  `scalaVersion`, not sbt's resolved value. It is a file-wide guess: a literal
  inside one project's settings also applies to the `%%` rows of every other
  project in the file; `crossScalaVersions` is ignored, so a cross-built
  project gets one suffix; and a suffixed version (`2.13.0-M5`, `3.0.0-RC1`,
  `2.13.12-bin-typelevel-4`) gets no hint, because sbt does not always shorten
  it to the x.y form.
- A `val` declared inside a block is not tracked. When it shares a name with a
  top-level `val`, a reference inside that block resolves to the top-level
  value, not the block-local one.
- Sbt rows do not yet join a registry package identity (#8961) and keep the
  line-keyed entity id (#8962).
