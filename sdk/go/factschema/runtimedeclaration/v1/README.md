# Runtime Declaration Fact Payloads (schema version 1)

This package holds the schema-version-1 typed payload struct for the
`runtime_declaration` fact family. A reducer or query handler never reads
`Envelope.Payload["some_key"]` for this kind directly; it decodes through the
parent `factschema` package's kind-keyed seam
(`factschema.DecodeRuntimeDeclarationVersion`) and receives this struct,
validated.

- Go import path: `github.com/eshu-hq/eshu/sdk/go/factschema/runtimedeclaration/v1`
- Module: `github.com/eshu-hq/eshu/sdk/go/factschema` (no `go/internal` imports)

## Purpose

One fact kind decodes through this package today:

| Fact kind | Struct | Decode function | Read by |
| --- | --- | --- | --- |
| `runtime_declaration.version` | `Version` | `factschema.DecodeRuntimeDeclarationVersion` | Nothing yet: the reducer projection is #8816 and the query tool is #8817 |

`Version` models one runtime version a repository declares in one file. The
git collector emits it from the version-pin files (`.nvmrc`, `.node-version`,
`.tool-versions`, `.sdkmanrc`, `rust-toolchain` and peers), the `engines`,
`volta` and `packageManager` sections of `package.json`, and the `go` and
`toolchain` directives of `go.mod`. Dockerfile and CI setup sources follow in a
later change through the same contract.

## Required vs. optional fields

| Struct | Required fields | Why |
| --- | --- | --- |
| `Version` | `RepoID`, `RelativePath`, `ScopeDir`, `LineNumber`, `OrderIndex`, `SourceKind`, `Runtime`, `RawSpec`, `SpecKind` | The collector always has all nine for a declaration it can read. They are the identity of the row and what an unguessed answer needs. |

Everything else is an omitempty pointer: the version parts, the range bounds,
`ToolName`, `Distribution` and `CollectorInstanceID`. An absent value decodes to
nil, never to zero. A missing required field is a classified `input_invalid`
error naming the field.

## Field semantics

- `SpecKind` is `exact`, `partial`, `range`, `alias` or `unresolved`. The set
  is closed for producers and tolerant for readers: the decoder does not check
  membership and the JSON Schema carries no enum, so a member added later still
  decodes. A reader that meets an unknown member keeps the row and treats it as
  not comparable.
- `VersionMajor`, `VersionMinor` and `VersionPatch` are set only for `exact`
  and `partial`. A range leaves them nil and sets `RangeLowerMajor`,
  `RangeUpperMajor`, the raw `RangeLower` and `RangeUpper`, and the two
  inclusive flags. For a disjunction such as `^14 || >=16` only the first lower
  bound is parsed, `RangeLower` is the whole spec and `RangeUpper` is nil.
- `ScopeDir` is the declaring file's directory: `.` at the repository root,
  otherwise slash separated, repository-relative and cleaned. It is never a
  governed workspace.
- `Runtime` is lower case and canonical for the known set (`node`, `go`,
  `python`, `ruby`, `java`, `rust`, `terraform`, `swift`). An unknown tool is carried
  verbatim and lower cased. `ToolName` holds the verbatim token when it
  differed (`nodejs` for `node`).
- `SourceKind` is snake case, an open set, and one value per declaring source.
  A version-pin file is one source, so it has one value even when the file has
  two spellings (`rust-toolchain` and `rust-toolchain.toml` are both
  `rust_toolchain`): `nvmrc`, `node_version`, `python_version`, `ruby_version`,
  `java_version`, `go_version`, `terraform_version`, `tool_versions`,
  `sdkmanrc`, `rust_toolchain`, `swift_version`. A manifest with several
  declaring sections has one value per section, named `<file>_<section>`:
  `package_json_engines`, `package_json_volta`, `package_json_package_manager`,
  `go_mod_go`, `go_mod_toolchain`. Later sources follow the convention, and a
  value is never renamed after it is emitted.
- `LineNumber` is 1-based. `OrderIndex` is the 0-based declaration on the line,
  so `nodejs 20.1.0 18.0.0` is two facts on one line, and a one-line
  `package.json` gives each of its declarations its own index.
- Aliases such as `lts/*`, `node` and `system` are never resolved offline.

## Changing this struct

Any field change here is a payload-schema change.

- **Additive optional field** (a new pointer/`omitempty` field): minor. Add it,
  regenerate and commit the schema in the same change.
- **Remove, rename or narrow a field, or change how the stable key
  `(repo_id, relative_path, line_number, runtime, order_index)` is derived**:
  major. It needs a conversion shim in the parent decode seam.

```bash
cd sdk/go/factschema
go generate ./...
cp schema/runtime_declaration.version.v1.schema.json fixturepack/schema/
```

`TestSchemasHaveNoDrift` fails the build on drift, and
`TestFixturePackSchemasMatchCanonical` locks the fixture-pack copy.

## Telemetry

None. This package has no runtime, network, queue, graph or telemetry path.

## Related docs

- `docs/internal/design/contract-system-v1.md` — §3.1 module layout, §3.2
  decode seam, §5 versioning.
- Parent module `README.md` (`sdk/go/factschema/README.md`).
- Issues #8815 and #8807.
