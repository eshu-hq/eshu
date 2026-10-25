# declaration (git hook)

## Purpose

`go/internal/collector/repo/git/runtime/declaration` is the Git collector hook
that emits `runtime_declaration.version` facts from version-pin files and from
`package.json` and `go.mod` (issue #8815).

## Where this fits

```
sync -> discover -> parse -> emit facts -> enqueue -> reducer -> projection -> query
                                    ^
                              this package
```

`git` drives the repository snapshot and the fact stream. It calls into this
package during emission; this package never calls back into `git`. The parsing
and classification live in `go/internal/collector/repo/runtime/declaration`.

## Exported surface

- `Candidate` — one admitted file held until emission. A pin file keeps its
  size, and its body only when the file is at or under
  `runtimedeclaration.MaxBytesPerFile`. A `package.json` or `go.mod` keeps only
  its `runtimedeclaration.Extraction` (a handful of declarations), never its
  body.
- `NoteRuntimeDeclarationCandidate` — records an admitted file during the
  content walk, on both the two-phase and the legacy branch. A pin body over
  the byte cap is recorded by size only. A manifest is parsed here and recorded
  only when it declares something or has a skip to report, so the common
  `package.json` with no `engines`, `volta` or `packageManager` leaves no entry.
- `EmitRuntimeDeclarationFactsForCandidates` — sends the facts through the
  shared writer, in path order. A pin file over
  `runtimedeclaration.MaxBytesPerFile` or `runtimedeclaration.MaxTokensPerFile`,
  a manifest over `runtimedeclaration.MaxManifestBytesPerFile`, and a
  `package.json` that is not readable emit nothing and are reported to the
  `onSkip` callback (relative path and a `runtimedeclaration.Skip`); the Git
  collector logs it as WARN `git_runtime_declaration_file_skipped`. A manifest
  that lost values to the credential guard emits the rest and is logged as WARN
  `git_runtime_declaration_values_rejected`. `onSkip` may be nil.

## Notes

Unlike CODEOWNERS there is no per-repository winner: every pin file, package.json
and go.mod in every directory declares its own scope, so a monorepo emits one
set of facts per package directory. A delta generation re-emits only the files
it changed.
A deleted pin file leaves its earlier facts in place until the projection
(#8816) retracts per repository on generation change; this package emits no
materialization marker fact.

## Verification

```bash
cd go && go test -race ./internal/collector/repo/git/... -count=1
```

Changes to emitted facts also need the golden-corpus gate (B-7) and the B-12
snapshot; see `docs/public/reference/local-testing/golden-corpus-gate.md`.
