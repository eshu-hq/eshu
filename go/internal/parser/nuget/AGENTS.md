# AGENTS.md - internal/parser/nuget guidance

## Read first

1. `README.md` - package purpose, ownership boundary, and emitted row fields.
2. `doc.go` - godoc contract for the MSBuild project-file parser.
3. `parser.go` - `Parse`, dependency row construction, MSBuild property
   resolution, and scope classification for `.csproj`.
3a. `props.go`, `props_scan.go`, `props_items.go`, `props_rows.go` - the
   fail-closed scanner for `Directory.Packages.props` and
   `Directory.Build.props`; `props_items.go` tracks the Update, Remove,
   duplicate, and unread constructs that demote a row (`item_ambiguity`). The
   grammar, reasons, and bounds are in `README.md`.
4. `parser_test.go` / `parity_test.go` - behavior coverage for package
   references, property substitution, item vs group conditions, ambiguous
   properties, and malformed XML. `props_*_test.go` cover the props scanner:
   grammar, limits and allocation ceilings, and a seeded oracle fuzz.
5. `../nuget_project_language.go` - the parent engine's dispatch wrapper.
6. `../json/nuget_lock.go` - the NuGet **lockfile** parser, which is a
   different package and a different file kind.

## Invariants this package enforces

- Dependency direction stays one way: the parent parser package may import
  this package, but this package must not import `internal/parser`. Only the
  external `nuget_test` package may, and it does so through `parsertest`.
- Never invoke MSBuild, restore packages, evaluate a `Condition` expression, or
  read a sibling file. A `$(Property)` only a sibling file defines must stay
  unresolved; that includes Central Package Management versions in
  `Directory.Packages.props`. Parsing a props file as the file under parse is
  allowed; completing a `.csproj` from one is not, and project
  `PackageReference` rows are never joined to central `PackageVersion` rows.
- The props scanner is fail-closed: a row is resolved only when the construct
  matches the README grammar, otherwise it is skipped or partial and counted in
  `msbuild_props_limits`. Never widen the grammar without a seeded-violation
  test, an oracle-fuzz case, and a mutation check.
- Every props bound is a count or byte length (`Max*` in `props.go`), asserted
  by allocation ceilings in `props_limits_test.go`, never by wall time.
- `value` never carries a version the file did not prove. When resolution
  fails, `value` keeps the declared text and the row is stamped
  `partial_evidence` with the matching `version_evidence`.
- `condition` stays byte-for-byte the item-override merge. `condition_item` and
  `condition_group` were added alongside it (#5725), not in place of it,
  because `go/internal/content/dependency_identity.go` reads all three.
- A `<PackageReference>` with neither `Include` nor `Update` is skipped. Never
  emit a row with no package name.
- Malformed XML returns an error rather than a partial payload.
- A props item row whose package MSBuild may change after the `Include`
  stays partial with an `item_ambiguity` reason; never resolve it by
  replaying the `Update`. Item-name tracking stays capped
  (`maxTrackedItemNames`), and the oracle fuzz must keep every reason
  occurring.
- `PackageVersion` rows keep `central_version_declaration: true` and
  `config_kind: dependency`. The reducer's `ExtractPackageManifestDependencies`
  drops them by that flag, and so do `IsCentralVersionDeclaration` (the
  Postgres consumption sidecar) and `NotCentralVersionDeclarationSQL` (the
  readiness `package.consumption` count) in `go/internal/storage/postgres`.
  Renaming or removing the flag turns every pin back into package consumption.
- Never unescape `%XX` in a props or `.csproj` value and never resolve an
  escaped spec or version (#8994 covers `.csproj`: an escaped spec emits no
  row, an escaped version is unresolved): MSBuild unescapes before matching,
  so the escaped text is not the
  identity. Keep `GlobalPackageReference` scope fixed at development unless
  NuGet.targets stops forcing `PrivateAssets="All"` on it.

## Common changes and how to scope them

- Add a failing test in `parser_test.go` before changing `parser.go`.
- New MSBuild element support belongs here only when the behavior is provable
  from one file. Anything that needs a sibling file or an evaluated condition
  is out of scope without an ADR.
- Registry dispatch, the `Options` bridge, and telemetry belong to the parent
  parser package.
- A row-field add or rename is a payload-shape change: check
  `go/internal/content/dependency_identity.go` and the reducer's NuGet
  correlation before landing it.

## Test placement

The tests are the external package `nuget_test` and reach the parser through
the real engine (`parsertest.MustParsePath`, or `parser.DefaultEngine` where
the malformed-XML error is asserted). They cannot be in-package: `parsertest`
imports `internal/parser`, and `internal/parser` imports this package, so an
in-package `nuget` test importing `parsertest` is an import cycle.

`test_helpers_test.go` keeps a local `assertBoolFieldValue` and a
`writeTestFile` that creates parent directories. Both stay local on purpose:
`parsertest`'s own AGENTS.md admits a shared helper only once two external
parser test packages need the same assertion, and this is currently the only
one needing a bool form.

## Failure modes and how to debug

- A missing row usually means the `<PackageReference>` had neither `Include`
  nor `Update`, or the element sat outside an `<ItemGroup>`.
- An unexpected `ambiguous_msbuild_property` means the same property name is
  declared with two different values across `<PropertyGroup>` elements. The
  parser ignores group `Condition`s, so a multi-targeted file legitimately
  produces this; that is the designed answer, not a bug to resolve by picking
  a branch.
- Two rows collapsing into one downstream usually means `condition_item` or
  `condition_group` was dropped from the row rather than a parser bug; check
  the identity discriminator first.
- Run with `-count=1` when iterating on fixtures so cached results do not hide
  a change.

## Anti-patterns specific to this package

- Picking a value for an ambiguous property, or defaulting a missing version.
- Evaluating `Condition` expressions to decide which `<ItemGroup>` "wins".
- Reading `Directory.Build.props` / `Directory.Packages.props` to complete a
  `.csproj` version (parsing them as their own files is the supported path).
- Registering a generic `.props` extension: only the two exact names bind.
- Parsing `packages.lock.json` here; that is `../json/nuget_lock.go`.

## What NOT to change without an ADR

- Cross-file MSBuild property or Central Package Management resolution.
- Filesystem or network lookups beyond the file passed to `Parse`.
- The `version_evidence` vocabulary or the `condition` / `condition_item` /
  `condition_group` triple; identity and docs depend on both.
