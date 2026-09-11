# call

The code-call family (issue #6061, moved under #6609; step 3 of
`docs/internal/design/reducer-target-tree.md`). Imported by the reducer root
as `codecall`.

## Ownership

This package turns parser `file` and `repository` facts into code-call rows:
it resolves each call site to a caller and callee code entity (per-language
resolvers, the shared `EntityIndex`, import and re-export resolution, Python
metaclass edges) and builds the `code_calls` shared-intent rows plus the
per-repo refresh intents that own the repo-wide CALLS retract. It writes
nothing itself. The root `CodeCallMaterializationHandler` persists the rows
through its `CodeCallIntentWriter`; the root code-call projection runner
drains them into graph edges.

## Files

47 non-test files and 90 test files. File names drop the `code_call_`,
`code_call_materialization_`, and `code_call_language_` prefixes the flat
root needed (`extract.go`, `intents.go`, `index.go`, `dart_resolver.go`, ...).

| File | Covers |
| --- | --- |
| `extract.go` | `ExtractRows`, `ExtractAllRelationshipRows`, `ExtractAllRelationshipRowsWithIndex`: codegraph decode, quarantine, row extraction |
| `index.go`, `index_types.go`, `index_helpers.go`, `index_rows.go` | `EntityIndex` and `BuildEntityIndex`: the shared code-entity substrate |
| `path_helpers.go` | `PathKeys`, `PayloadInt`, path normalization |
| `intents.go` | `BuildSharedIntentRows`, `BuildRefreshIntentsWithDeltaFileScopes`, partition keys, `RepoRefreshEvidenceSource` |
| `file_scope.go` | `BuildFileScopesByRepoID`, `AcceptanceScanLimit` |
| `resolver.go`, `*_resolver.go`, `*_index.go` | Per-language call resolution |
| `python_metaclass.go` | `ExtractPythonMetaclassRows` |
| `parsed_file_data.go` | Typed `parsed_file_data` reads (gomod module path, dead-code root kinds) |

## Seams the reducer root uses

- Extraction: `ExtractAllRelationshipRowsWithIndex`, `ReferencedSymbolKeys`.
- Intents: `BuildSharedIntentRows`, `BuildRefreshIntentsWithDeltaFileScopes`,
  `BuildFileScopesByRepoID`, `FileScopeBuildResult`, `DeltaFileScope`.
- Entity resolution for the symbol-runtime families: `EntityIndex`,
  `BuildEntityIndex`, `ResolveContainingEntityID`, `EndpointEntityType`,
  `PathKeys`, `PayloadInt`.
- Runner vocabulary: `PartitionKeyVersion`, `PayloadBool`,
  `RefreshPartitionKey`, `RefreshPartitionKeyForDelta`,
  `WholeScopePartitionKey`, `AcceptanceScanLimit`,
  `RepoRefreshEvidenceSource`, `EvidenceSource`,
  `PythonMetaclassEvidenceSource`.

## Dependency rule

One-way imports only: the shared tier (`contract`, `factload`, `factdecode`,
`schemadecode`, `sharedintent`, `payloadcore`) plus `facts`, the SDK
`factschema`, and the standard library. Never the parent reducer package.
