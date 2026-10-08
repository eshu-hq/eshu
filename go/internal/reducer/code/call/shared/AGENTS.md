# shared — agent instructions (issue #6061)

This directory is the code-call substrate. Read `doc.go` and `README.md`
before changing `EntityIndex` or any of the generic candidate-name/path
helpers every language leaf depends on.

## Invariants

- Never import `code/call` or any `code/call/<language>` leaf. This package
  sits below the dispatcher and every leaf; an import back up would be a
  cycle. `go list -deps` (or `rg` for `code/call/` imports in this
  directory) must show none.
- `EntityIndex` is built once per pass by `BuildEntityIndex` and read-only
  afterward. Do not add mutation methods. Its language-specific lookup
  fields (the ones listed in `README.md`) stay unexported; add a read-only
  accessor method when a leaf needs a new one, never a new exported field —
  the field-export alternative was rejected precisely because it drops the
  read-only invariant across the package boundary.
- Verify a new or changed accessor still inlines:
  `go build -gcflags=-m ./internal/reducer/code/call/... 2>&1 | rg 'inlining call to .*EntityIndex'`
  should list it. If it stops inlining, the resolution hot path pays for
  every lookup a leaf makes; investigate before landing.
- Exported names here never repeat the `codeCall` stutter the flat package
  carried (`shared.ExactCandidateNames`, not
  `shared.CodeCallExactCandidateNames`). A type or function that crosses a
  leaf boundary only through a returned value (e.g. `goCrossRepoExportEntry`,
  `javaScriptStaticAliasSpan`) keeps its unexported type name but exports its
  fields — a leaf receives a value via `:=` and reads exported fields without
  ever spelling the type.
- Test doubles for another package's fixtures stay minimal and clearly
  test-only (`RepositoryImportPathsByRepo`, `ImportedTargets`,
  `MatchImportedPath`): they exist because `code/call`'s test suite
  (T-a: dispatcher tests stay in `code/call`) needs them, not because
  production code does.
- Span and alias lookups stay repository- and file-scoped (#7640). Store and
  probe `spansByFile`, `containersByFile`, and `javaScriptAliasesByFile`
  only by repository ID plus the `FileKeys` values. Never key a containment
  lookup by a bare file name (`PathKeys` includes one) or by a path from
  another repository, and never build a `repo+path` string per probe on this
  hot path. `containment_test.go` holds the regression table.
- Same-file callee lookups stay repository- and file-scoped too (#7642).
  Store and probe `entitiesByRepoPathLine`, `uniqueNameByRepoPath`, and
  `constructorByRepoPath` only by repository ID plus the `FileKeys` values,
  through `ResolveEntityID`, `UniqueNameByRepoPath`,
  `ResolveSameFileCalleeEntityID`, and `ResolveConstructorMethodCalleeID`.
  `uniqueNameByPath` keeps its bare and aggregated keys only for
  cross-file import lookups that probe an explicitly matched target path.
  `same_file_scope_test.go` holds the regression tables.
- Prove changes with `go test ./internal/reducer/code/call/... -count=1`
  (recursive covers every leaf) plus the resolution goldens in `code/call`
  when row shapes could change.
