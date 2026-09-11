# shared

The code-call entity-index substrate (issue #6061, split from `code/call`
per the language-nesting proposal). Not imported outside `code/call` and its
language leaves.

## Ownership

This package owns `EntityIndex` — the built lookup structure every
per-language resolver, the code-call row builders, and the reducer root's
`handles_route`, `runs_in`, `invokes_cloud_action`, and symbol-runtime
builders resolve code entities through — plus the generic candidate-name
derivation, path normalization, import/re-export resolution, and the
per-language index contributors `BuildEntityIndex` calls while scanning
`file` facts (the Go export/method-return index, the JavaScript static-alias
cache, the Python/Rust/TypeScript index contributors). It writes nothing and
holds no dispatch logic — the ordered resolver dispatch and per-language
wiring stay in `code/call`.

## Files

22 non-test files.

| File | Covers |
| --- | --- |
| `index.go`, `index_types.go`, `index_helpers.go` | `EntityIndex`, `BuildEntityIndex`, `FunctionSpan` |
| `context.go` | `Phase`, `Resolver`, `ResolveContext` |
| `names.go` | `ExactCandidateNames`, `BroadCandidateNames`, candidate-name derivation |
| `paths.go` | `PathKeys`, `PayloadInt`, `NormalizePath`, `CallLanguage`, `HasQualifiedScope` |
| `arity.go` | `AppendArityNames`, `AppendTypedSignatureNames`, `MetadataInt`, `MetadataStringSlice` |
| `containment.go` | `ResolveContainingEntityID` |
| `endpoint_types.go` | `EndpointEntityType` |
| `imports.go`, `import_targets.go`, `import_guards.go` | Repository-import caching, import-target derivation, Python import-binding barriers |
| `reexports.go` | `ReexportIndex`, `BuildReexportIndex` |
| `symbol_index.go` | Stable-symbol-key resolution, `ReferencedSymbolKeys` |
| `receiver_method_index.go` | `ResolveReceiverMethodCallee` (Swift/JavaScript repo-scoped receiver typing) |
| `parsed_file_data.go` | Typed `parsed_file_data` reads (gomod module path, dead-code root kinds) |
| `python_index.go`, `rust_index.go`, `typescript_index.go` | Per-language index contributors `BuildEntityIndex` calls |
| `go_index.go` | Cross-repo Go package-export index build |
| `javascript_aliases.go` | Static JavaScript alias-set scanning cached per function source |

## EntityIndex accessors

`EntityIndex`'s language-specific lookup fields stay unexported so the
read-only invariant survives the package boundary. A language leaf reads them
through: `EntityFileByID`, `UniqueNameByRepoDir`, `GoMethodReturnTypes`,
`GoExportByImportPath`, `JavaScriptAliasesByPath`, `PythonClassBasesByRepo`,
`RustTraitMethodsByRepo`, `SpansByPath`, `TypeScriptInterfaceMethodsByRepo`.
`UniqueNameByPath` and `UniqueNameByRepo` are exported fields (unchanged from
before the split). Every accessor call inlines
(`go build -gcflags=-m` reports `inlining call to shared.EntityIndex.<Accessor>`
at every call site), so the accessor indirection costs nothing on the
resolution hot path — verified by a before/after benchmark on
`BenchmarkExtractCodeCallRowsLargeJavaScriptDynamicCalls`.

## Dependency rule

This package imports only the shared reducer tier (`contract`, `factload`,
`factdecode`, `schemadecode`, `sharedintent`, `payloadcore`) and, outside the
reducer, `facts`, `codeprovenance`, the SDK `factschema`, and the standard
library. It never imports `code/call` or any `code/call/<language>` leaf.
