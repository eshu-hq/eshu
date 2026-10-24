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
- `dependency_scope`: `compile`, `test`, `provided`, or `runtime`. Custom
  configurations map to `compile`; the raw spelling stays in `sbt_configuration`.
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

## Bounds

One forward pass, no recursion, one token of pushback: work is linear in the
input for a 100 KB single line, 200k nested parentheses, or 100k dependency
lines. Rows cap at `MaxDependencyRows` (10,000); the excess is counted, not
stored. A coordinate field over `MaxFieldBytes` (512) is not a coordinate. At
most 1,024 top-level string vals and 64 distinct Scala versions are tracked.

## What this parser never does

- Evaluate Scala, run sbt, or read `project/*.sbt`, `plugins.sbt`,
  `Build.scala`, `Dependencies.scala`, or any sibling file.
- Resolve `.value` settings, interpolated strings, helper objects, or
  conditional (`if`) and block-bodied collections. These are counted in
  `unresolved_declaration_count`.
- Resolve a version through a `val` declared inside a block or after the
  reference's own statement is parsed; only top-level literal vals resolve, at
  the end of the file, so declaration order does not matter.
- Emit a row for `addSbtPlugin`, `dependsOn`, resolvers, or `crossScalaVersions`.
