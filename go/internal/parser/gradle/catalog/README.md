# Gradle Version Catalog Parser

## Purpose

This package turns a Gradle version catalog (`gradle/libs.versions.toml`) into
dependency rows for the supply-chain impact reducer. A catalog is where a JVM
build pins most of its library versions, so the build-script parser alone sees
only the aliases (`libs.guava`) and never the coordinate or version.

## Ownership boundary

The package owns catalog parsing only. The parent parser package owns registry
dispatch (`registry_definitions.go` binds the exact name `libs.versions.toml`
to the `gradle` definition) and the engine hook
(`go/internal/parser/gradle_language.go`, which routes by base name). The
build-script parser stays in `go/internal/parser/gradle`. The reducer
(`go/internal/reducer/packages/correlation/consumption.go`) owns matching rows
to package-registry identity.

## Grammar

The parser is fail-closed. A row is `resolved` only when the file spells the
version out under one of the forms below. Everything else is counted, never
guessed.

| Table | Entry form | Row |
| --- | --- | --- |
| `[versions]` | `alias = "1.2.3"` | defines a plain version for `version.ref` |
| `[versions]` | `alias = { strictly = ..., require = ..., prefer = ..., reject = ... }` | defines a rich version; a reference to it is `unresolved` |
| `[libraries]` | `alias = "group:artifact:version"` | `dependency`, `resolved` |
| `[libraries]` | `alias = "group:artifact"` | `dependency`, `partial` (no version) |
| `[libraries]` | `{ module = "g:a", version = "1" }` or `{ group = "g", name = "a", version = "1" }` | `dependency`, `resolved` |
| `[libraries]` | `version.ref = "key"` or `version = { ref = "key" }` | `resolved` when `key` is a plain `[versions]` string, else `unresolved` |
| `[libraries]` | `version = { strictly = ... }` or `version.<other> = ...` | `unresolved`, key `version` |
| `[plugins]` | `alias = { id = "x.y", version = ... }` or `alias = "x.y:1.0"` | `gradle_plugin`, same version rules |
| `[bundles]` | `alias = ["lib-a", "lib-b"]` | counted, no row |

Coordinate parts and plugin ids use letters, digits, `.`, `_`, `-`. A version
text must be printable ASCII without whitespace, quotes, backslash, `$`,
backtick, braces, `<`, `>`, `|`, `;` or `&`. A range such as `[1.0,2.0)` or a
dynamic version such as `1.+` passes and is recorded as written, not
interpreted. An alias, coordinate part or version over 256 bytes is
unsupported, never truncated.

Outside the grammar, each construct adds one to `unsupported_count` and emits
no row: a root-level key, a `[libraries.alias]` sub-table, a `[[array]]` table,
an unknown table such as `[metadata]`, a dotted alias, an unknown key in an
inline table, `module` together with `group`/`name`, a non-string value, a
string notation with more than three parts.

Not valid and returns no rows (`state: malformed`): a TOML syntax error, a
repeated table header, or a repeated alias in one table. A file over 2 MiB
(`oversize`) or nested deeper than 16 levels (`too_deep`) is not parsed.

## Exported surface

- `Parse(path, isDependency, options) (map[string]any, error)`: payload with a
  `variables` bucket and a `gradle_catalog_state` envelope.
- `Scan(source []byte) Result`: the same work on bytes, used by the tests.

Each row carries `name` (`group:artifact` or the plugin id), `value` (empty
unless resolved), `line_number`, `section` (`libraries` or `plugins`),
`config_kind` (`dependency` or `gradle_plugin`), `package_manager` and `lang`
(`gradle`), `dependency_resolution_state`, `dependency_unresolved_keys` (when
unresolved), `dependency_path_kind` (`manifest`), `catalog_alias`, and
`version_ref` (when a reference was used).

A catalog entry declares a dependency; it does not prove a build script uses
it. Rows therefore set no `direct_dependency` and no `dependency_scope`. Two
aliases for the same coordinate and version share one content-entity identity,
which is one declaration.

`gradle_catalog_state` holds `state`, the counts (`version_count`,
`library_count`, `plugin_count`, `bundle_count`, `resolved_count`,
`partial_count`, `unresolved_count`, `unsupported_count`, `dropped_count`),
and `parse_error` when the state is not `parsed`.

## Bounds

All work is linear in the input. The limits are constants in `types.go`:

| Limit | Value | Over the limit |
| --- | --- | --- |
| File size | 2 MiB | `oversize`, no parse |
| Array and inline-table depth | 16 | `too_deep`, no parse |
| Rows, version aliases and bundle aliases per file | 5000 each | counted in `dropped_count` |
| Alias, coordinate part, plugin id, version text | 256 bytes | entry unsupported |

The depth bound exists because the TOML parser recurses once per nesting level:
measured on `go-toml/v2` v2.3.1, a one-million-level input ends in a fatal
stack overflow, and a 256 KiB input of 262144 `[` allocates about 77 MB.
`nestingExceeds` rejects such input first with one linear pass.

## What this parser never does

- Execute Gradle or read `settings.gradle`, so a catalog registered under a
  name other than `libs.versions.toml` (`dependencyResolutionManagement`
  `versionCatalogs`) is not found.
- Interpret version ranges, pick a version from a rich version, or follow a
  `files(...)` or `from(...)` catalog import.
- Match a plugin id to a package registry: `gradle_plugin` rows are ignored by
  the package reducer.

## Telemetry

No-Observability-Change: the package emits no metrics, spans or logs. It runs
inside the parent engine's per-file parse, which owns parse timing and the
`parser` counters. An operator finds a catalog the parser could not read in the
file fact's `gradle_catalog_state`: `state` other than `parsed`, or non-zero
`unresolved_count`, `unsupported_count` or `dropped_count`.

## Performance

No-Regression Evidence: the new code runs only for `libs.versions.toml` and
adds no work to build-script parsing. `BenchmarkParseConfig` on a 10,001-line
input, three runs each, `-benchtime=100x`, same host:

| Case | ns/op | B/op | allocs/op |
| --- | --- | --- | --- |
| `gradle` (build.gradle, closest existing parser) | 55,163,320 to 55,457,359 | 12.8 MB | 119,750 |
| `gomod` | 14,554,327 to 14,682,101 | 13.7 MB | 179,804 |
| `gradle_catalog` (this package) | 9,623,034 to 9,675,085 | 10.6 MB | 133,551 |

## Verification

`go test -race -count=1 ./internal/parser/gradle/catalog` runs the grammar
fixtures, the allocation ceilings (bytes and objects relative to input size, no
wall time), and two seeded generators. One checks resolved rows against a
grammar oracle across 3000 catalogs. The other mutates the positive fixture
4000 times and checks that no row names text absent from the source.
