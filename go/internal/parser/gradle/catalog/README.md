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
separator: `a-b`, `a_b` and the quoted key `"a.b"` normalize to one alias.
A `version.ref` is normalized the same way before its lookup, so
`version.ref = "kotlin-core"` finds `kotlin_core`.

Gradle reads every key of a table (its `keySet`), so these forms register
`alias` too, though the parser emits no row for them: a dotted entry
(`alias.module = "g:a"`, `alias.version.ref = "v"`), a `[libraries.alias]`
sub-table and its deeper tables, an `[[libraries.alias]]` array table, and an
alias longer than 256 bytes. The same holds in `[versions]`, `[plugins]` and
`[bundles]`. The parser registers each such alias in its table, so it
collides with the other spellings of it like any alias. Writing one alias as
several dotted keys (`alias.module` and `alias.version`) is one alias.

Gradle also generates one accessor method per alias. Its `toJavaName` splits
an alias on `-`, `_` and `.` and capitalizes each part, and it fails the build
when two aliases in a table map to one name: `fooBar`, `foo-bar` and `foo.bar`
are all `getFooBar()`. The parser treats such aliases as colliding too. A
`version.ref` still finds only an alias with its normalized spelling:
`version.ref = "foo-bar"` does not find `fooBar`.

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
  `alias_collision_count`, including a dotted or sub-table alias that emits
  no row. An alias dropped past the row cap still marks a collision with an
  alias the table kept.

The same alias spelled twice exactly (`a` and `a`, or `a` and `"a"`) is a
repeated TOML key, which TOML forbids and Gradle rejects, so the whole file is
`malformed`. Both rules fail closed; they differ only because one input is
invalid TOML and the other is valid TOML that Gradle resolves by an order the
file does not carry.

### Unsupported and malformed input

Outside the grammar, each construct adds one to `unsupported_count` and emits
no row: a root-level key, a `[libraries.alias]` sub-table, a `[[array]]` table,
an unknown table such as `[metadata]`, a dotted alias, an alias over 256 bytes,
an unknown key in an inline table, `module` together with `group`/`name`, a
non-string value, a string notation with the wrong part count. A sub-table,
array table, dotted or over-long alias in a catalog table still collides with
the other spellings of its alias (see Aliases).

Not valid and returns no rows (`state: malformed`): a TOML syntax error, or
any key or table TOML forbids defining twice, in any table, the root, or an
inline table. That covers a repeated header or alias, `x = ...` with
`x.module = ...` or a `[libraries.x]` sub-table, a table defined by dotted
keys and again by a header, a `[versions]` alias that is a string and a rich
version at once, a dotted key that extends a table a header defined, `[x]`
and `[[x]]` for one name, and a repeated key in an inline table. A key tree
(`keys.go`) checks every expression as the streaming parse yields it, with
go-toml's rules; the test reference is go-toml's full decode into a map. A
value the TOML scanner accepts but go-toml's decoder rejects is malformed
too, wherever it sits (any table, the root, an inline table or an array):
an integer past int64 (`99999999999999999999`, `0x8000000000000000`), a
leading zero, a float out of range (`1e400`), a date that does not exist
(`1979-02-30`, `1979-13-01`), an hour of 24, a minute or second of 60, an
offset past `+23:59`, or a local time with an offset. `validScalar`
(`scalars.go`) follows go-toml v2.3.1's decode checks; strings and booleans
skip it. A
legal spread of one alias stays parsed: `x.module` with `x.version`, a
deeper `[libraries.x.version]` table after `x.module`, a sub-table before its
super-table, and repeated `[[x]]` elements. A file over 2 MiB (`oversize`) or nested deeper than 16
levels (`too_deep`) is not parsed. A key with more than 64 dotted segments,
or a file with more than 262,144 key segments in all, is also `too_deep`.

### Known limits

These fail closed (no row or no version), but they are coverage gaps:

- Gradle's dotted entry form, `alias.module = "g:a"` with
  `alias.version.ref = "v"`, and the `[libraries.alias]` sub-table form are
  counted as unsupported and emit no row. They still collide with other
  spellings of the alias.
- Some catalogs fail Gradle's build, yet the parser still emits rows for the
  entries it can read: two aliases with one accessor name (the parser marks
  only those two `unresolved`), a bundle that names a library the catalog does
  not define, and a quoted alias that contains `.` (`"a.b" = ...`). Gradle's
  TOML reader looks each alias up again as a dotted path, so it finds no entry
  for `"a.b"` and stops with a missing-group error.
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
| Dotted segments in one key | 64 | `too_deep`, no rows |
| Key segments in the file, headers and inline keys included | 262,144 | `too_deep`, no rows |
| Rows, version aliases and bundle aliases per file | 5000 each | counted in `dropped_count` |
| Alias, coordinate part, plugin id, version text | 256 bytes | entry unsupported |

The depth bound exists because the TOML parser recurses once per nesting level:
measured on `go-toml/v2` v2.3.1, a one-million-level input ends in a fatal
stack overflow, and a 256 KiB input of 262144 `[` allocates about 77 MB.
`nestingExceeds` rejects such input first with one linear pass.

The key-segment bounds exist because the streaming parse builds one node
per segment and the key tree keeps one entry (about 70 bytes) per distinct
segment. A 1.8 MB file of one 900,000-segment key allocated 715.7 MB with
the earlier semantic decode and 250.2 MB now that the key is refused first
(`TestScanLongDottedKeyAllocationStaysBounded`); those 139 bytes per input
byte are the streaming parse, which builds every node of an expression before
the scanner can count them. The file bound caps the parsed worst case: 2 MiB
of unique 64-segment headers allocated 271.6 MB and parsed before, and is now
`too_deep` at 38.5 MB. The costliest file inside the bounds, unique
64-segment headers up to 262,144 segments, allocates 40.6 MB
(`TestScanFileKeySegmentBoundIsExactlyMaxFileKeySegments`). One inline table
of 150,000 keys allocates 150.2 MB, 127 MB of it the streaming parse (254.5 MB
before). A byte-level guard that counts dotted segments before the parser
would remove the streaming-parse cost; it is a follow-up to the shared nesting
guard in [#8992](https://github.com/eshu-hq/eshu/pull/8992).

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
catalog (3,335 entries per table), `-benchtime=100x`. The build-script rows
are from an earlier session; the first three catalog rows are three
interleaved runs each in one later session, and the last row is six runs
interleaved with a rebuild of the row above, in a third session on the same
host under a load average of 7 to 18:

| Case | ns/op | B/op | allocs/op |
| --- | --- | --- | --- |
| `gradle` (build.gradle, closest existing parser) | 55,001,279 to 55,237,630 | 12.7 MB | 119,733 to 119,749 |
| `gomod` | 14,755,776 to 14,874,295 | 13.7 MB | 179,804 |
| `gradle_catalog` before alias normalization and the TOML check | 9,403,277 to 9,500,783 | 10.63 MB | 133,552 |
| `gradle_catalog` with flat alias collisions only | 10,931,196 to 11,101,267 | 11.13 MB | 133,606 |
| `gradle_catalog` before the key tree (empty-struct decode) | 10,925,659 to 11,089,130 | 10.89 MB | 123,601 |
| `gradle_catalog` (this package, key tree) | 11,476,968 to 11,880,070 | 11.56 MB | 133,638 |

The correctness checks cost about 15% on this input (medians 9.48 ms and
10.93 ms): mostly the semantic TOML decode, then the alias table. The key
tree replaces that decode, which skipped every key under a table header. In
six runs interleaved with the row above (base 11,010,758 to 11,824,432 ns/op
in that session), the medians are 11.37 ms and 11.63 ms (+2.3%), with 0.67 MB
and 10,037 allocations more per file: one index entry and one map key for
each key outside a small inline table. Registering dotted and sub-table
aliases, keying slots on accessor names, and the key segment walk cost
nothing measurable; reading only the first key segments
instead of building a slice per key saves 10,005 allocations. A real catalog
of a few hundred lines costs tens of microseconds. The scalar value check
(`scalars.go`) cost +1.0% (medians 11.53 ms and 11.64 ms over five
interleaved pairs) with no change in allocations: the catalog holds only
strings, which return after one switch.

Every alias-table check is one map operation. `BenchmarkScanManySpellingsOfOneAlias`
writes 15,000 spellings of one normalized alias (5,000 kept, 10,000 dropped):
a slice scan of earlier spellings took 132.5 ms per file, and the map takes
9.4 ms.

## Verification

`go test -race -count=1 ./internal/parser/gradle/catalog` runs the grammar
fixtures, the allocation ceilings (bytes and objects relative to input size, no
wall time), and four seeded generators:

- a grammar oracle across 3000 catalogs;
- a Gradle model across 3000 catalogs (set `ESHU_CATALOG_MODEL_CASES` for a
  longer run). It draws colliding alias spellings (separators and camel
  case), dotted-entry and sub-table aliases that emit no row, dynamic and
  exact versions, and wrong part counts. It models Gradle's keySet walk, its
  normalization and its accessor names, and checks every row: a collision
  never resolves and a selector never resolves;
- a key-redefinition generator across 3000 catalogs (`ESHU_CATALOG_MODEL_CASES`
  too). It repeats and redefines keys across the dotted, header, array-table
  and inline forms in every table and the root, and mixes in integer, float
  and date values from `scalarCases`, legal and not. It requires `malformed`
  with no rows exactly when go-toml's full decode into a map rejects the
  file, `parsed` otherwise;
- 4000 byte-level mutations of the positive fixture. No row may name text
  absent from the source, and no resolved value may be a selector.
