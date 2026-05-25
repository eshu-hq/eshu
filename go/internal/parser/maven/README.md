# Maven Parser

## Purpose

This package owns repository-side Maven `pom.xml` dependency parsing. It
turns each `pom.xml` into `content_entity`-shaped dependency rows the
supply-chain impact reducer can correlate against package-registry Maven
identities (`packageidentity.EcosystemMaven`).

## Ownership boundary

The package owns Maven manifest parsing only. The parent parser package owns
registry dispatch, file discovery, and the engine wrapper
(`go/internal/parser/maven_language.go`). The reducer
(`go/internal/reducer/package_consumption_correlation.go`) owns matching the
emitted rows to package-registry identities.

## Exported surface

- `Parse(path string, isDependency bool, options shared.Options) (map[string]any, error)`.

The returned payload follows `shared.BasePayload(path, "maven", isDependency)`
with a `variables` bucket containing one row per provable dependency. Each
row carries:

- `name`: `groupId:artifactId` Maven coordinate.
- `value`: resolved version when a local `<properties>` entry satisfies the
  reference, otherwise the raw `${property}` literal.
- `section`: `dependencies`, `dependencies:test`, `dependencies:provided`,
  `dependencyManagement`, `dependencyManagement:import`, or
  `profiles:<id>:dependencies[:<scope>]`.
- `config_kind`: always `dependency`.
- `package_manager`: always `maven`.
- `dependency_scope`: lowercase Maven scope; defaults to `compile`.
- `dependency_resolution_state`: `resolved`, `partial` (no `<version>`), or
  `unresolved` (unresolved `${property}` reference).
- `dependency_unresolved_keys`: present only when state is `unresolved`.
- `dependency_optional`: boolean from `<optional>`.
- `dependency_type`, `dependency_classifier`: when declared.
- `direct_dependency`: always `true` (manifests do not encode transitives).
- `dependency_path_kind`: always `manifest`.

## Dependencies

Imports only `encoding/xml`, `regexp`, `sort`, `strings`, and
`internal/parser/shared`. Must not import the parent parser package,
collectors, storage, query, or reducer code.

## What this parser never does

- Execute Maven or download artifacts.
- Resolve parent POMs across files (multi-module references stay
  `unresolved`).
- Invent versions when `<version>` is missing or a property cannot be
  satisfied from the same file's `<properties>`.

## Telemetry

This package emits no telemetry. Parse timing remains owned by the parent
parser engine; the existing `parser` package counters cover the new file
patterns.

## Verification

`go test ./internal/parser/maven -count=1` runs the parse fixture matrix.
The shared dependency coverage gate
`go test ./internal/parser/json -run TestDependencyCoverage -count=1` also
exercises `pom.xml` end-to-end and locks the matrix entry to a covered
fixture so the docs claim cannot drift away from parser behavior.
