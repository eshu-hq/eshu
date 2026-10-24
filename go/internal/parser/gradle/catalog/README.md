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
| `[libraries]` | `{ module = "g:a" }` or `{ group = "g", name = "a" }` | `dependency`, `partial` (no version) |
| `[libraries]` | `{ module = "g:a", version = "1" }` or `{ group = "g", name = "a", version = "1" }` | `dependency`, `resolved` |
| `[libraries]` | `version.ref = "key"` or `version = { ref = "key" }` | `resolved` when `key` names exactly one plain `[versions]` string, else `unresolved` |
| `[libraries]` | `version = { strictly = ... }` or `version.<other> = ...` | `unresolved`, key `version` |
| `[plugins]` | `alias = { id = "x.y", version = ... }` or `alias = "x.y:1.0"` | `gradle_plugin`, same version rules |
| `[bundles]` | `alias = ["lib-a", "lib-b"]` | counted, no row |

Coordinate parts and plugin ids use letters, digits, `.`, `_`, `-`. A library
string needs exactly three parts and a plugin string exactly two: Gradle fails
the build on any other count, so `"g:a"` and `"x.y"` are unsupported.

### Versions

A row is `resolved` only when its version text is one exact version. The text
must be printable ASCII without whitespace, quotes, backslash, `$`, backtick,
braces, `<`, `>`, `|`, `;` or `&`. It must also not be something Gradle reads
as a selector or a modifier:

| Text | Gradle reads it as | Row |
| --- | --- | --- |
| `1.+`, `+` | dynamic version | `unresolved`, key `version` |
| `latest.release`, `latest.integration` (any case) | status selector | `unresolved`, key `version` |
| `[1.0,2.0)`, `]1.0,2.0[`, `(,2.0]`, `1.0,2.0` | range | `unresolved`, key `version` |
| `1.0!!`, `[1.0,2.0)!!1.5`, any `!` | strict version (`1.0!!` is strictly 1.0) | `unresolved`, key `version` |
| `1.0@jar` | version plus artifact type | `unresolved`, key `version` |

The rule is the same for a literal `version = "..."`, the third part of a
string notation, and the `[versions]` string a `version.ref` names; through a
reference the key is the reference. An alias, coordinate part or version over
256 bytes is unsupported, never truncated.

### Aliases

Gradle's `AliasNormalizer` treats `-`, `_` and `.` in an alias as one
separator: `a-b`, `a_b` and the quoted key `"a.b"` are one alias to Gradle.
A `version.ref` is normalized the same way before its lookup, so
`version.ref = "kotlin-core"` finds `kotlin_core`.

Two aliases with one normalized spelling in the same table are valid TOML.
Gradle accepts them with a log warning: in `[versions]` it keeps the first
registration, and in `[libraries]`, `[plugins]` and `[bundles]` the later
entry replaces the earlier one. Gradle registers aliases in key-length order,
not file order, so the file does not show which one survives. The parser
never guesses:

- every library or plugin row whose alias collides is `unresolved` with no
  value and key `catalog_alias`;
- a `version.ref` to a colliding `[versions]` alias is `unresolved`, keyed by
  the reference;
- every colliding alias, in any of the four tables, adds one to
  `alias_collision_count`. An alias dropped past the row cap still marks a
  collision with an alias the table kept.

The same alias spelled twice exactly (`a` and `a`, or `a` and `"a"`) is a
repeated TOML key, which TOML forbids and Gradle rejects, so the whole file is
`malformed`. Both rules fail closed; they differ only because one input is
invalid TOML and the other is valid TOML that Gradle resolves by an order the
file does not carry.

### Unsupported and malformed input

Outside the grammar, each construct adds one to `unsupported_count` and emits
no row: a root-level key, a `[libraries.alias]` sub-table, a `[[array]]` table,
an unknown table such as `[metadata]`, a dotted alias, an unknown key in an
inline table, `module` together with `group`/`name`, a non-string value, a
string notation with the wrong part count.

Not valid and returns no rows (`state: malformed`): a TOML syntax error, a
repeated table header, a repeated alias in one table, or any other input TOML
rejects, such as a table defined by dotted keys and again by a header. After
the streaming parse succeeds, a decode into an empty struct applies TOML's
semantic checks. A file over 2 MiB (`oversize`) or nested deeper than 16
levels (`too_deep`) is not parsed.

### Known limits

These fail closed (no row or no version), but they are coverage gaps:

- Gradle's dotted entry form, `alias.module = "g:a"` with
  `alias.version.ref = "v"`, is counted as unsupported and emits no row.
- Gradle's alias validation (allowed characters, the reserved `extensions`,
  `convention` and `class` names, and the reserved `versions`, `plugins` and
  `bundles` library prefixes) is not applied. A catalog Gradle rejects for an
  alias name still yields rows.
- A file that starts with a UTF-8 byte-order mark is `malformed`, because the
  TOML parser does not accept one. Whether Gradle accepts it is not checked.

## Exported surface

- `Parse(path, isDependency, options) (map[string]any, error)`: payload with a
  `variables` bucket and a `gradle_catalog_state` envelope.
- `Scan(source []byte) Result`: the same work on bytes, used by the tests.

Each row carries `name` (`group:artifact` or the plugin id), `value` (empty
unless resolved), `line_number`, `section` (`libraries` or `plugins`),
`config_kind` (`dependency` or `gradle_plugin`), `package_manager` and `lang`
(`gradle`), `dependency_resolution_state`, `dependency_unresolved_keys` (when
unresolved), `dependency_path_kind` (`manifest`), `catalog_alias`, and
`version_ref` (the reference as written, when one was used).

A catalog entry declares a dependency; it does not prove a build script uses
it. Rows therefore set no `direct_dependency` and no `dependency_scope`. Two
aliases for the same coordinate and version share one content-entity identity,
which is one declaration.

`gradle_catalog_state` holds `state`, the counts (`version_count`,
`library_count`, `plugin_count`, `bundle_count`, `resolved_count`,
`partial_count`, `unresolved_count`, `unsupported_count`, `dropped_count`,
`alias_collision_count`), and `parse_error` when the state is not `parsed`.
`alias_collision_count` overlaps the others: a colliding library row also
counts in `unresolved_count`.

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

- Execute Gradle or read `settings.gradle`. A catalog that `settings.gradle`
  registers under another file name (`versionCatalogs`) is not found, because
  only the exact name `libs.versions.toml` is admitted.
- Interpret version ranges or dynamic versions, pick a version from a rich
  version, pick the survivor of an alias collision, or follow a `files(...)`
  or `from(...)` catalog import.
- Match a plugin id to a package registry: `gradle_plugin` rows are ignored by
  the package reducer.

## Telemetry

No-Observability-Change: the package emits no metrics, spans or logs. It runs
inside the parent engine's per-file parse, which owns parse timing and the
`parser` counters. An operator finds a catalog the parser could not read in the
file fact's `gradle_catalog_state`: `state` other than `parsed`, or non-zero
`unresolved_count`, `unsupported_count`, `dropped_count` or
`alias_collision_count`.

## Performance

No-Regression Evidence: the code runs only for `libs.versions.toml` and adds
no work to build-script parsing. `BenchmarkParseConfig` on a 10,005-line
catalog (3,335 entries per table), three runs each, `-benchtime=100x`, one
session on one host:

| Case | ns/op | B/op | allocs/op |
| --- | --- | --- | --- |
| `gradle` (build.gradle, closest existing parser) | 55,001,279 to 55,237,630 | 12.7 MB | 119,733 to 119,749 |
| `gomod` | 14,755,776 to 14,874,295 | 13.7 MB | 179,804 |
| `gradle_catalog` before alias normalization and the TOML check | 9,525,233 to 9,666,101 | 10.6 MB | 133,551 |
| `gradle_catalog` without the TOML semantic check | 9,912,846 to 9,956,195 | 11.1 MB | 133,597 to 133,598 |
| `gradle_catalog` (this package) | 10,989,581 to 11,106,454 | 11.1 MB | 133,605 to 133,606 |

The two correctness checks cost about 15% on this input: the semantic TOML
decode about 1.1 ms and the normalized alias table about 0.4 ms. A real
catalog of a few hundred lines costs tens of microseconds. The decode
allocates almost nothing, because it decodes into an empty struct.

## Verification

`go test -race -count=1 ./internal/parser/gradle/catalog` runs the grammar
fixtures, the allocation ceilings (bytes and objects relative to input size, no
wall time), and three seeded generators:

- a grammar oracle across 3000 catalogs;
- a Gradle model across 3000 catalogs (set `ESHU_CATALOG_MODEL_CASES` for a
  longer run). It draws colliding alias spellings, dynamic and exact versions,
  and wrong part counts, and checks every row against Gradle's normalization
  rule: a collision never resolves and a selector never resolves;
- 4000 byte-level mutations of the positive fixture. No row may name text
  absent from the source, and no resolved value may be a selector.
