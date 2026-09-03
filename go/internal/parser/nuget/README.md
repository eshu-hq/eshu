# NuGet Project Parser

## Purpose

This package owns repository-side MSBuild project-file (`.csproj`) dependency
parsing. It turns each project file into `content_entity`-shaped dependency
rows the supply-chain impact reducer can correlate against package-registry
NuGet identities.

## Ownership boundary

The package owns `.csproj` manifest parsing only. The parent parser package
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
- Read `Directory.Build.props`, `Directory.Packages.props`, or any other
  sibling file. Central Package Management versions declared outside the
  project file therefore stay unresolved.
- Invent a version when one is missing or a property cannot be satisfied from
  the same file.

A file whose XML does not decode returns an error; the engine surfaces it
rather than emitting a partial payload.

## Telemetry

This package emits no telemetry. Parse timing stays owned by the parent parser
engine.

## Verification

`go test ./internal/parser/nuget -count=1` runs the four behavior tests, all of
which drive the real engine through `parsertest.MustParsePath` (or
`parser.DefaultEngine` for the malformed-XML error) rather than calling `Parse`
directly, so registry dispatch is covered along with the parse itself.

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
