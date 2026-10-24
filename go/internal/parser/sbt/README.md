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
coordinate (`"a" % "b" % "1" % Test`), after a parenthesised coordinate
(`("a" %% "b" % "1").cross(...) % Test`), or on a collection
(`Seq(...).map(_ % Test)`, `Seq(...) map (_ % "test")`). Without one it comes
from the `Test /` axis or the legacy `libraryDependencies in Test` form, else
`compile`. A `.map(...)` whose body has no `%` (`_.withSources()`) changes no
scope.

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

A scope the scanner cannot read is never a resolved `compile`. A custom
`Config` value, a variable such as `% testConfig`, an unrecognised string, a
configuration string over `MaxConfigBytes` (256), a
`.map(dep => dep % Test)` lambda, or `libraryDependencies in (a, Test)` keeps
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
