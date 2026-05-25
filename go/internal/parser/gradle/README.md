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
(`go/internal/reducer/package_consumption_correlation.go`) owns matching the
emitted rows to package-registry identities.

## Exported surface

- `Parse(path string, isDependency bool, options shared.Options) (map[string]any, error)`.

The returned payload follows `shared.BasePayload(path, "gradle", isDependency)`
with a `variables` bucket containing one row per provable dependency. Each
row carries:

- `name`: `groupId:artifactId` Maven coordinate.
- `value`: resolved version when a local `def`/`val`/`ext` declaration
  satisfies the interpolation, otherwise the raw `$var`/`${var}` literal.
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
- `dependency_unresolved_keys`: present only when state is `unresolved`.
- `direct_dependency`: always `true`.
- `dependency_path_kind`: always `manifest`.

## Dependencies

Imports only the Go standard library and `internal/parser/shared`. Must not
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
