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
| `paths.go` | `PathKeys` (bare-name keys included, for name lookups), `PayloadInt`, `NormalizePath`, `CallLanguage`, `HasQualifiedScope` |
| `arity.go` | `AppendArityNames`, `AppendTypedSignatureNames`, `MetadataInt`, `MetadataStringSlice` |
| `containment.go` | `ResolveContainingEntityID`, `FileKeys`, `NarrowestContainingSpan` (repository- and file-scoped span lookup, #7640) |
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

`EntityIndex`'s lookup fields are all unexported so the read-only invariant
survives the package boundary. A language leaf reads them through:
`EntityFileByID`, `UniqueNameByPath`, `UniqueNameByRepo`, `UniqueNameByRepoDir`,
`GoMethodReturnTypes`, `GoExportByImportPath`, `HasGoExports`,
`JavaScriptAliasesByFile`, `PythonClassBasesByRepo`, `RustTraitMethodsByRepo`,
`SpansByFile`, `TypeScriptInterfaceMethodsByRepo`, and (for tests)
`RepositoryImportPathsByRepo`. Every accessor call inlines
(`go build -gcflags=-m` reports `inlining call to shared.EntityIndex.<Accessor>`
at every call site), so the accessor indirection costs nothing on the
resolution hot path — verified by a before/after benchmark on
`BenchmarkExtractCodeCallRowsLargeJavaScriptDynamicCalls`.

## Containment is file-scoped (#7640)

A call's containing function comes only from the call file's own identity:
(repository, normalized full path) and (repository, normalized relative path).
`spansByFile`, `containersByFile`, and `javaScriptAliasesByFile` are nested
`repository -> file key -> spans` maps written only under the two
`FileKeys` values, and `ResolveContainingEntityID`, `SpansByFile`, and
`JavaScriptAliasesByFile` take a repository ID. Before this, spans were also
stored under the bare file name from `PathKeys`, so a top-level call (no
enclosing span in its own file) picked the narrowest span over that line in
any same-named file, including files of other repositories. The probe builds no
composite `repo+path` string: it normalizes the two paths (no allocation on an
already-clean path) and does at most two nested map lookups, with no slice
allocation where `PathKeys` built one.

Consequences: a top-level call now has a caller only when a fallback supplies
one (a JavaScript/TypeScript package-root file, a JavaScript reference or
same-file top-level call, or a Java metadata root, as before). Any other
top-level call, in any language, has no caller and emits no `CALLS` row. PHP in-function calls also drop until the PHP parser
reports real `end_line` values, because today a PHP function span is zero-width.
`PathKeys` still includes bare names for the name lookups (`ResolveEntityID`,
`uniqueNameByPath`) that this change did not touch.

## Dependency rule

This package imports only the shared reducer tier (`contract`, `factload`,
`factdecode`, `schemadecode`, `sharedintent`, `payloadcore`) and, outside the
reducer, `facts`, `codeprovenance`, the SDK `factschema`, and the standard
library. It never imports `code/call` or any `code/call/<language>` leaf.
