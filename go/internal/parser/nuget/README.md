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

- `name`: the `Include` attribute, falling back to `Update`. A spec that holds
  a `%XX` escape (a percent sign and two hex digits) emits no row (#8994):
  MSBuild unescapes it before matching, so the escaped text is not the
  package identity. This is the props scanner's wildcard rule.
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
- `version_evidence`: one of `package_reference` (literal version, where a
  `%` not followed by two hex digits is literal text),
  `project_property` (every `$(Property)` resolved), `unresolved_msbuild_property`,
  `ambiguous_msbuild_property`, or `missing_version`. A version that holds a
  `%XX` escape, as declared or after property substitution, is
  `unresolved_msbuild_property` with the declared text in `value` (#8994).

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
streaming `encoding/xml` token scanner in `props.go`, `props_scan.go`,
`props_items.go`, and `props_rows.go`. Both files accept the same grammar; the file name does not
change what is read.

### Grammar (fail-closed)

A row is resolved only when the construct matches the grammar below. Anything
else is skipped or partial, and counted. Element and attribute names are read
case-sensitively. MSBuild matches them case-insensitively, so a wrongly cased
item type (`packagereference`) is skipped, counted, and demotes every row of
its type, and a wrongly cased attribute (`version=`) makes its item a conflict.
A wrongly cased `update=` or `remove=` also skips its item and demotes every
earlier row of its type as updated or removed, because MSBuild still applies it
to earlier items.

- Root element must be `Project`. A different root, a second root, an empty
  document, or malformed XML (unterminated tag, comment or CDATA, mismatched
  end tag, undefined entity) returns an error and no partial payload.
- Direct children of `Project`: `PropertyGroup` and `ItemGroup` are read.
  Every other child (`Import`, `ImportGroup`, `Choose`, `ItemDefinitionGroup`,
  `Target`, ...) is skipped and counted in `skipped_elements`, so a
  declaration inside a `Choose/When` is never guessed. Any such child, or an
  `Sdk` attribute on `Project`, also demotes every item row (see below).
- `ItemGroup` items read: `PackageVersion`, `GlobalPackageReference`,
  `PackageReference`. Other item types are ignored. Attributes read: `Include`,
  `Update`, `Remove`, `Exclude`, `Version`, `VersionOverride`, `PrivateAssets`,
  `IncludeAssets`, `ExcludeAssets`, `Condition`. The same five metadata names
  may be child elements (matched case-insensitively), as in a `.csproj`.
  MSBuild applies child metadata after the attribute, so the last child wins.
- An item is skipped and counted in `skipped_items` when it has no `Include`,
  has `Update` or `Remove` (alone or with `Include`), names more than one
  package (`;`, `*`, `?`), names a package through `$(`, `@(` or `%(`, repeats
  an attribute or carries a case-variant one, gives an attribute and a child
  element that disagree (an empty child over a set attribute counts), or
  carries both `Version` and `VersionOverride`.
- `PropertyGroup` children are leaf elements. A child with nested elements is
  skipped, counted, and makes the property unresolvable.

### Property precedence

Properties are keyed case-insensitively, as MSBuild does. A `$(Name)` in a
version resolves only when every declaration of `Name` in the file is
unconditional (no `Condition` on the group or the element), non-empty, free of
`$(`, `@(`, `%(` and `%XX` escapes, and identical. There is no chaining and no last-wins
override:

| Declarations of the name | Reference result |
| --- | --- |
| one unconditional literal, or identical repeats | resolved, `project_property` |
| any `Condition`, or two different values | `ambiguous_msbuild_property`, `partial_evidence` |
| empty, an expression, nested, or oversized | `unresolved_msbuild_property`, `partial_evidence` |
| not declared in the file | `unresolved_msbuild_property`, `partial_evidence` |

After substitution, a version that still contains `$(` (for example a property
function) stays unresolved and keeps the declared text.

MSBuild unescapes `%XX` (a percent sign and two hex digits) in item specs and
metadata before it uses them, so `Newtonsoft%2EJson` is `Newtonsoft.Json`. The
scanner does not unescape. A version or property value with an escape is
unresolved (`unresolved_msbuild_property`; a property row says
`msbuild_expression`), and an `Include`, `Update`, or `Remove` spec with an
escape is treated like a wildcard (see the table below). A percent sign not
followed by two hex digits is literal text, as it is to MSBuild. `value` is the literal
text exactly as written whenever nothing was substituted.

### Item operations (`item_ambiguity`)

MSBuild can change an item after its `Include`. When the file shows such a
construct, the row stays but is demoted: it carries `item_ambiguity` (the
reasons below, sorted and comma-joined) and `partial_evidence: true`. Its
`value` and `version_evidence` are unchanged, so a reader still sees what the
file declared.

| Reason | When |
| --- | --- |
| `conditional_metadata` | a metadata child (`<Version Condition=...>`) has a `Condition` |
| `duplicate_include` | another `Include` of the same type names the package (case-insensitive, NU1506), including an item skipped for another reason |
| `updated` | a later `Update` of the same type names the package, or is a list part, wildcard, expression, or `%XX` escape |
| `removed` | the same for a later `Remove` |
| `exclude` | the item has an `Exclude` attribute |
| `unresolved_item_spec` | an `Include` of the same type is a list, wildcard, expression, `%XX` escape, or over 256 bytes, or an item type is wrongly cased |
| `unresolved_asset_metadata` | the `PrivateAssets`/`IncludeAssets` value that would decide `dependency_scope` is conditioned, an expression, or escaped; `dependency_scope` is then absent |
| `unevaluated_element` | the file has any `Project` child other than `PropertyGroup`/`ItemGroup`, or `Project` has `Sdk` |
| `central_management_unproven` | the file sets `ManagePackageVersionsCentrally` to anything but one unconditional literal `true` (case-insensitive); `PackageVersion` and `GlobalPackageReference` rows only |

`Update` and `Remove` follow MSBuild evaluation order: they affect only items
of the same type defined before them. An `Include` after a `Remove` re-adds
the package, but the re-added row is partial with `duplicate_include`: the
tracker counts every literal `Include` of a name, so it stays on the safe
side. An absent
`ManagePackageVersionsCentrally` proves nothing either way and changes no row.
The duplicate tracker remembers at most 10,000 distinct names; past that,
every row of the affected type gets `duplicate_include`, because uniqueness
can no longer be proven.

### Rows

Item rows keep the `.csproj` row contract (`name`, `value`,
`requested_version`, `version_evidence`, the condition triple, scope fields)
with these differences:

- `section` is the item type: `PackageVersion`, `GlobalPackageReference`, or
  `PackageReference`.
- `PackageVersion` rows carry `central_version_declaration: true` and no
  `direct_dependency`: a central version is a policy, not proof any project
  references the package. The other two carry `direct_dependency: true`. The
  reducer's `ExtractPackageManifestDependencies`
  (`go/internal/reducer/packages/correlation/consumption.go`) skips every row
  with `central_version_declaration: true`, so a pin is never package
  consumption, a `DEPENDS_ON` edge, or security-alert evidence. The row keeps
  `config_kind: dependency` for its section-keyed content identity, so every
  reader that counts dependency facts must drop it too. Two more do: the
  Postgres `package_manifest_consumption_keys` sidecar never stores a pin
  (`IsCentralVersionDeclaration` in `go/internal/storage/postgres`), and the
  supply-chain readiness `package.consumption` count filters pins with the SQL
  form, `NotCentralVersionDeclarationSQL`. A repository whose only dependency
  rows are pins therefore reads `evidence_incomplete` with `owned_packages`
  missing, not `ready_zero_findings`.
- A `VersionOverride` with no `Version` is used as the version and also kept in
  `version_override`.
- A row with a group or item `Condition` is `conditional_declaration: true` and
  `partial_evidence: true`, even when its version is literal.
- `dependency_scope`: a `GlobalPackageReference` is `development`
  (`test` by name). NuGet.targets builds a `PackageReference` from it with
  `PrivateAssets="All"` and the development `IncludeAssets` set on the
  element, and element metadata wins over the item's own, so declared asset
  metadata cannot change it. `PackageVersion` and `PackageReference` rows use
  the `.csproj` name and `PrivateAssets`/`IncludeAssets` heuristics, but only
  from unconditional values that need no evaluation. When a conditioned,
  expression, or escaped value could change the answer, `dependency_scope`,
  `development_dependency`, and `test_dependency` are absent and the row
  carries `unresolved_asset_metadata`.

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

### Known limits

- `$(Name)` resolves only from this file's `PropertyGroup`s. A property set by
  an imported file, the SDK, or the command line stays unresolved, and an
  imported file's items are never seen (the `Import` demotes every row).
- Conditions are never evaluated. A conditional item stays partial; two
  `Include`s of one package under opposite conditions are both partial
  duplicates, which is the honest answer for multi-targeting.
- Two same-case duplicate `Include`s without conditions share one content
  identity (`go/internal/content/dependency_identity.go` keys NuGet rows by
  section, name, and the condition pair), so the content store keeps one of
  them. Both rows are `duplicate_include` partial, so the kept row never
  claims a resolved version.
- A `Target` cannot change what NuGet restore reads at evaluation time in the
  common case, but this scanner does not prove that, so a `Target` demotes
  every row like any other unread element.

## Performance and observability

No-Regression Evidence: the props path is new and runs only for the two exact
file names, so `.csproj` parsing is unchanged. `BenchmarkParseConfig` at the
10,002-line floor (`-benchtime=30x`, a shared 16-thread host, mean of three
rounds alternating with the scanner before the escape and scope rules) measured
`nuget_project` (.csproj) 28.0 ms/op, 25.0 MB B/op, 289,787 allocs/op,
`nuget_directory_packages_props` 26.2 ms/op, 20.7 MB B/op, 222,406 allocs/op
(25.6 ms, 20.7 MB, 222,407 allocs before), and `nuget_directory_build_props`
28.9 ms/op, 22.6 MB B/op, 244,887 allocs/op (28.3 ms, 22.6 MB, 244,885 allocs
before). The escape check and scope rule add about 2% time and no allocations.
The item-operation tracker before them added about 14% time, 14% bytes, and 5%
allocations (22.2 ms, 18.0 MB, 212,340 allocs and 24.3 ms, 19.9 MB, 234,820
allocs without it). The props files stay at or under the `.csproj` parser.
Allocation ceilings in `props_limits_test.go` bound a 10 MiB single line,
90,000 entries, 500,000 nested tags, and unterminated tags, comments and CDATA
by input size, and a doubling test requires allocation to grow at most 2.3x for
2x entries. The item-operation tracker adds a map of at most 10,000 names: a
4 MiB file of Includes, named and wildcard Updates, and Removes allocates
67.2 MB (16.0x input; 27.9x under `-race`). One element with 349,505 repeated
`Version` attributes on 4.19 MB allocates 94.3 MB (22.5x), and one with 300,000
distinct attributes on 3.49 MB allocates 93.2 MB (26.7x; 27.4x under `-race`),
mostly the XML decoder's attribute slice. All stay under the 32x ceiling.

No-Observability-Change: the parser emits no spans, metrics, or logs, like the
rest of this package. An operator sees skipped and capped constructs in the
`msbuild_props_limits` payload bucket and in the stored rows' `partial_evidence`.
Parse timing stays in the parent engine's `FileParseDuration`.

## Telemetry

This package emits no telemetry. Parse timing stays owned by the parent parser
engine.

## Verification

`go test ./internal/parser/nuget -count=1` runs the `.csproj` behavior tests and
the props tests: grammar, item operations, limits and allocation ceilings, a
3,000-case seeded oracle fuzz, and a 2,000-case mutated-document survival
test. The oracle models every `item_ambiguity` reason and fails if one stops
occurring; `ESHU_PROPS_FUZZ_CASES=30000` runs a deeper pass. The behavior
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
