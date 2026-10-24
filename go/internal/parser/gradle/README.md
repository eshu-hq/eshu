# Gradle Parser

## Purpose

This package owns repository-side Gradle build-script dependency parsing for
both Groovy DSL (`build.gradle`) and Kotlin DSL (`build.gradle.kts`). It
turns each build script into `content_entity`-shaped dependency rows the
supply-chain impact reducer can correlate against package-registry Maven
identities (`packageidentity.EcosystemMaven`, since Gradle resolves through
Maven repositories).

## Ownership boundary

The package owns Gradle build-script parsing only. The parent parser package
owns registry dispatch, file discovery, and the engine wrapper
(`go/internal/parser/gradle_language.go`). The reducer
(`go/internal/reducer/packages/correlation/consumption.go`) owns matching the
emitted rows to package-registry identities.

## Exported surface

- `Parse(path string, isDependency bool, options shared.Options) (map[string]any, error)`.

The returned payload follows `shared.BasePayload(path, "gradle", isDependency)`
with a `variables` bucket containing one row per provable dependency. Each
row carries:

- `name`: `groupId:artifactId` Maven coordinate.
- `value`: resolved version when a local `def`/`val`/`ext` declaration
  satisfies the interpolation, otherwise the raw `$var`/`${var}` literal.
  A Gradle selector keeps the text as written (see below).
- `section`: configuration name (e.g. `implementation`,
  `testImplementation`, `runtimeOnly`), prefixed with parent block when
  inside `buildscript`/`subprojects`/`allprojects`, and suffixed with
  `:platform`/`:enforcedPlatform` for BOM wrappers.
- `config_kind`: always `dependency`.
- `package_manager`: always `gradle`.
- `dependency_scope`: Maven-style scope derived from configuration
  (`compile`, `test`, `runtime`, `provided`, `annotationProcessor`,
  `classpath`, or `platform`).
- `dependency_resolution_state`: `resolved`, `partial`, or `unresolved`.
- `dependency_unresolved_keys`: the interpolation keys the file cannot
  satisfy. Present only when state is `unresolved` because of them; a
  selector row has none.
- `direct_dependency`: always `true`.
- `dependency_path_kind`: always `manifest`.

## Version selectors

A row is `resolved` only when its version, after same-file interpolation,
passes `selector.Exact` (`go/internal/parser/gradle/selector`). A Gradle
selector names no single version: the prefix form `1.+`, `latest.release`,
a Maven-style range `[1.0,2.0)`, the strict short-hand `1.0!!`, and the
artifact-only suffix `1.0@jar`. Such a row is `unresolved` and keeps the
version text as written in `value`, so the reducer sees the requested range
and never an observed version (issue #8988). The rule fails closed: an
interior `+` (`1.0.0+build.1`) or text outside printable ASCII also stays
unresolved. Every notation goes through the same check: Groovy string and
map, Kotlin string and named arguments, `kotlin()`, `platform()`, and an
interpolated property. Rich-version closures (`version { strictly(...) }`)
are not read and stay `partial`.

String notation is `group:name[:version[:classifier]][@ext]`. The classifier
and the extension name the artifact, so neither joins `name`, and the
classifier never joins `value`: `g:a:1.0:jdk8` is `resolved` at `1.0`. An
`@ext` suffix stays on a declared version (`g:a:1.0:jdk8@jar` keeps
`1.0@jar`, `unresolved`) and is dropped when there is no version
(`g:a@jar` is `partial` with name `g:a`). Text past the classifier is not a
Gradle notation; it is kept as the version, where its `:` keeps the row
`unresolved`.

## Dependencies

Imports only the Go standard library, `internal/parser/shared`, and
`internal/parser/gradle/selector`. Must not
import the parent parser package, collectors, storage, query, or reducer
code.

## What this parser never does

- Execute Gradle, evaluate Groovy or Kotlin, or call out to the Gradle
  daemon.
- Run source-set, plugin, or version-catalog resolution. Version catalogs
  (`libs.versions.toml`) require a separate parser entry.
- Read sibling modules, `settings.gradle`, or `settings.gradle.kts`. Each
  build script is parsed in isolation.
- Invent coordinates for `project(":x")`, `files(...)`, `fileTree(...)`, or
  Gradle-internal helpers.

## Telemetry

This package emits no telemetry. Parse timing remains owned by the parent
parser engine; the existing `parser` package counters cover the new file
patterns.

## Verification

`go test ./internal/parser/gradle -count=1` runs the parse fixture matrix.
The shared dependency coverage gate
`go test ./internal/parser/json -run TestDependencyCoverage -count=1` also
exercises `build.gradle` and `build.gradle.kts` end-to-end and locks the
matrix entries to covered fixtures.
