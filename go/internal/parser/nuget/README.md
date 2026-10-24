# NuGet Project Parser

## Purpose

This package owns repository-side MSBuild project-file (`.csproj`) dependency
parsing, plus the two shared props files `Directory.Packages.props` and
`Directory.Build.props` (#8824). It turns each file into `content_entity`-shaped
dependency rows the supply-chain impact reducer can correlate against
package-registry NuGet identities.

## Ownership boundary

The package owns `.csproj` and the two exact-name props files only (the
registry binds those names, never a generic `.props` extension). The parent parser package
owns registry dispatch, file discovery, and the engine wrapper
(`go/internal/parser/nuget_project_language.go`, which is only
`parseNuGetProject` calling `Parse`). NuGet **lockfiles**
(`packages.lock.json`) are JSON and belong to
`go/internal/parser/json/nuget_lock.go`, not here. Row-to-entity identity
(including the discriminator that keeps two same-name PackageReference rows
apart) lives in `go/internal/content/dependency_identity.go`.

## Exported surface

- `Parse(path string, isDependency bool, options shared.Options) (map[string]any, error)`.

The returned payload follows `shared.BasePayload(path, "nuget_project",
isDependency)` with a `variables` bucket carrying one row per
`<PackageReference>`. `options.IndexSource` adds the raw file text under
`source`; no other option is read. Each row always carries:

- `name`: the `Include` attribute, falling back to `Update`.
- `line_number`: the row's 1-based position in emission order. It is a
  sequence number, **not** the XML line the reference was written on.
- `value`: the version this parser could prove. Equal to `requested_version`
  when no `$(Property)` reference is involved, or when one could not be
  resolved.
- `requested_version`: the version text exactly as declared, `""` when the
  reference declares none.
- `section`: always `PackageReference`.
- `config_kind`: always `dependency`.
- `package_manager`: always `nuget`.
- `dependency_scope`: `test`, `development`, or `runtime` (see below).
- `direct_dependency`: always `true` (a project file declares no transitives).
- `lang`: always `nuget_project`.
- `version_evidence`: one of `package_reference` (literal version),
  `project_property` (every `$(Property)` resolved), `unresolved_msbuild_property`,
  `ambiguous_msbuild_property`, or `missing_version`.

These are present only when the file proves them:

- `version_property` / `version_properties`: the resolved MSBuild property
  name, or the list when a version interpolates more than one.
- `unresolved_msbuild_property` / `ambiguous_msbuild_property`: comma-joined
  property names that blocked resolution.
- `partial_evidence`: `true` alongside every non-`package_reference`,
  non-`project_property` evidence value.
- `private_assets`, `include_assets`, `exclude_assets`: the attribute or child
  element value, trimmed; omitted when empty.
- `condition`, `condition_item`, `condition_group`: see below.
- `development_dependency` (scope `development` or `test`) and
  `test_dependency` (scope `test`).

## Version resolution

`$(Property)` references are substituted only from `<PropertyGroup>` elements
in the same file, and every `<PropertyGroup>` contributes regardless of its
`Condition` — the parser does not evaluate MSBuild conditions. A property
declared twice with two different values is therefore **ambiguous**: the row
keeps the raw text and is marked partial rather than picking a branch. A
property that never appears is **unresolved**. Both keep `value` equal to the
declared text so a reader sees exactly what the file said.

## Conditions

`condition` is the pre-merged item-override value (item-level `Condition`
wins, group-level is the fallback) and is preserved byte-for-byte for existing
display and identity consumers. `condition_item` and `condition_group`
additionally expose the two components separately (#5725) so the identity
discriminator can keep two same-name `PackageReference` rows distinct when
they share an item-level `Condition` but sit under `ItemGroup`s with different
group-level conditions. Empty components are omitted, so a row with only one
component carries only that key.

## Dependency scope

- `test` when the lowercased name is `xunit`, `nunit`, `mstest.testframework`,
  `microsoft.net.test.sdk`, or `coverlet.collector`, or simply contains
  `test`. This is a name heuristic, not proof from the file.
- `development` when `PrivateAssets` lists `all` or `IncludeAssets` lists
  `none` (`;`, `,`, or space separated, case-insensitive).
- `runtime` otherwise.

## Dependencies

Imports only `bytes`, `encoding/xml`, `fmt`, `regexp`, `strings`, and
`internal/parser/shared`. Must not import the parent parser package,
collectors, storage, query, or reducer code.

## What this parser never does

- Invoke MSBuild, restore packages, or perform network lookups.
- Evaluate `Condition` expressions.
- Read a sibling file. A `.csproj` is never completed from a props file, so
  Central Package Management versions declared outside the project file stay
  unresolved there. A props file is parsed only as the file under parse.
- Join project `PackageReference` rows to central `PackageVersion` rows. The
  central declarations are their own rows (section `PackageVersion`); linking
  them to projects needs cross-file correlation and an ADR.
- Invent a version when one is missing or a property cannot be satisfied from
  the same file.

A file whose XML does not decode returns an error; the engine surfaces it
rather than emitting a partial payload.

## Shared props files (Directory.Packages.props, Directory.Build.props)

`Parse` routes the two exact file names (matched case-insensitively) to a
streaming `encoding/xml` token scanner in `props.go`, `props_scan.go`, and
`props_rows.go`. Both files accept the same grammar; the file name does not
change what is read.

### Grammar (fail-closed)

A row is resolved only when the construct matches the grammar below. Anything
else is skipped or partial, and counted. Element and attribute names are
case-sensitive; a wrongly cased `packagereference` is skipped and counted.

- Root element must be `Project`. A different root, a second root, an empty
  document, or malformed XML (unterminated tag, comment or CDATA, mismatched
  end tag, undefined entity) returns an error and no partial payload.
- Direct children of `Project`: `PropertyGroup` and `ItemGroup` are read.
  Every other child (`Import`, `Choose`, `Target`, ...) is skipped and counted
  in `skipped_elements`, so a declaration inside a `Choose/When` is never
  guessed.
- `ItemGroup` items read: `PackageVersion`, `GlobalPackageReference`,
  `PackageReference`. Other item types are ignored. Attributes read: `Include`,
  `Update`, `Version`, `VersionOverride`, `PrivateAssets`, `IncludeAssets`,
  `ExcludeAssets`, `Condition`. The same five metadata names may be child
  elements (matched case-insensitively), as in a `.csproj`.
- An item is skipped and counted in `skipped_items` when it has no `Include`,
  has `Update` (alone or with `Include`), names more than one package
  (`;`, `*`, `?`), names a package through `$(`, `@(` or `%(`, repeats an
  attribute, gives an attribute and a child element that disagree, or carries
  both `Version` and `VersionOverride`.
- `PropertyGroup` children are leaf elements. A child with nested elements is
  skipped, counted, and makes the property unresolvable.

### Property precedence

Properties are keyed case-insensitively, as MSBuild does. A `$(Name)` in a
version resolves only when every declaration of `Name` in the file is
unconditional (no `Condition` on the group or the element), non-empty, free of
`$(`, `@(` and `%(`, and identical. There is no chaining and no last-wins
override:

| Declarations of the name | Reference result |
| --- | --- |
| one unconditional literal, or identical repeats | resolved, `project_property` |
| any `Condition`, or two different values | `ambiguous_msbuild_property`, `partial_evidence` |
| empty, an expression, nested, or oversized | `unresolved_msbuild_property`, `partial_evidence` |
| not declared in the file | `unresolved_msbuild_property`, `partial_evidence` |

After substitution, a version that still contains `$(` (for example a property
function) stays unresolved and keeps the declared text. `value` is the literal
text exactly as written whenever nothing was substituted.

### Rows

Item rows keep the `.csproj` row contract (`name`, `value`,
`requested_version`, `version_evidence`, the condition triple, scope fields)
with these differences:

- `section` is the item type: `PackageVersion`, `GlobalPackageReference`, or
  `PackageReference`.
- `PackageVersion` rows carry `central_version_declaration: true` and no
  `direct_dependency`: a central version is a policy, not proof any project
  references the package. The other two carry `direct_dependency: true`.
- A `VersionOverride` with no `Version` is used as the version and also kept in
  `version_override`.
- A row with a group or item `Condition` is `conditional_declaration: true` and
  `partial_evidence: true`, even when its version is literal.
- `dependency_scope` uses the same name and `PrivateAssets`/`IncludeAssets`
  heuristics as `.csproj`.

Property rows use `section: PropertyGroup`, `config_kind: msbuild_property`,
carry no `package_manager`, and set `value_evidence` to `literal`,
`conditional`, `ambiguous`, `msbuild_expression`, `empty`, or `oversized`;
anything but `literal` is `partial_evidence`. `value` is the declared text, or
`""` when oversized.

### Bounds

All bounds are counts or byte lengths (constants in `props.go`) and every
excess is counted in the payload bucket `msbuild_props_limits`
(`input_over_limit`, `depth_exceeded`, `skipped_elements`, `skipped_items`,
`oversized_fields`, `dropped_item_rows`, `dropped_property_rows`):

| Bound | Value | On excess |
| --- | --- | --- |
| input | 4 MiB | no rows, `input_over_limit` |
| element depth | 64 | no rows, `depth_exceeded` |
| item rows | 5000 | extra items counted in `dropped_item_rows` |
| property names and rows | 2000 | extras counted; a dropped declaration still marks a kept name ambiguous |
| package or property name | 256 bytes | item skipped or property dropped, `oversized_fields` |
| version text | 256 bytes | item skipped, `oversized_fields` |
| property value, asset list | 1024 bytes | `oversized_fields` |
| Condition | 1024 bytes | `oversized_fields` |

`encoding/xml` does not expand custom entities, so no entity bomb is possible;
depth is capped by this scanner before the decoder stack grows.

## Performance and observability

No-Regression Evidence: the props path is new and runs only for the two exact
file names, so `.csproj` parsing is unchanged. `BenchmarkParseConfig` at the
10,002-line floor (`-benchtime=30x`, one run on a shared 16-thread host)
measured `nuget_project` (.csproj) 28.0 ms/op, 25.0 MB B/op, 289,791 allocs/op,
`nuget_directory_packages_props` 22.3 ms/op, 18.0 MB B/op, 212,339 allocs/op,
and `nuget_directory_build_props` 24.3 ms/op, 19.9 MB B/op, 234,819 allocs/op.
The props files stay under the closest existing parser. Allocation ceilings in
`props_limits_test.go` bound a 10 MiB single line, 90,000 entries, 500,000
nested tags, and unterminated tags, comments and CDATA by input size, and a
doubling test requires allocation to grow at most 2.3x for 2x entries.

No-Observability-Change: the parser emits no spans, metrics, or logs, like the
rest of this package. An operator sees skipped and capped constructs in the
`msbuild_props_limits` payload bucket and in the stored rows' `partial_evidence`.
Parse timing stays in the parent engine's `FileParseDuration`.

## Telemetry

This package emits no telemetry. Parse timing stays owned by the parent parser
engine.

## Verification

`go test ./internal/parser/nuget -count=1` runs the `.csproj` behavior tests and
the props tests: grammar, limits and allocation ceilings, a 3,000-case seeded
oracle fuzz, and a 2,000-case mutated-document survival test. The behavior
tests drive the real engine through `parsertest.MustParsePath` (or
`parser.DefaultEngine` for the malformed-XML error), so registry dispatch is
covered along with the parse itself. The limit and fuzz tests call `Parse`
directly, because they measure the parser's own allocation.

Two guards live outside this package and are not re-asserted here:

- `go/internal/parser/engine_single_read_test.go`'s
  `TestParsePathReadsNuGetProjectSourceExactlyOnce` proves a `.csproj` parse
  performs exactly one physical disk read per `ParsePath` call. It stays with
  the engine because the priming that collapses the two reads lives in
  `ParsePath`.
- `go/internal/parser/json/dependency_coverage.go` carries the `*.csproj` row
  behind [Dependency Coverage](../../../../docs/public/reference/dependency-coverage.md)
  and names this package as its `SourceReference`.
  `TestDependencyCoverage` there asserts the row stays `Covered` and cites some
  non-empty source; it does not check that the cited path exists, so a rename
  in this package must repoint that constant and the published table by hand.
