# declaration (git hook)

## Purpose

`go/internal/collector/repo/git/runtime/declaration` is the Git collector hook
that emits `runtime_declaration.version` facts from version-pin files (issue
#8815).

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

- `NoteRuntimeDeclarationCandidate` — records the body of an admitted pin file
  during the content walk, on both the two-phase and the legacy branch.
- `EmitRuntimeDeclarationFactsForCandidates` — sends the facts through the
  shared writer, in path order. A pin file over
  `runtimedeclaration.MaxBytesPerFile` or `runtimedeclaration.MaxTokensPerFile`
  emits nothing and is reported to the `onSkip` callback (relative path and a
  `runtimedeclaration.Skip`); the Git collector logs it as WARN
  `git_runtime_declaration_file_skipped`. `onSkip` may be nil.

## Notes

Unlike CODEOWNERS there is no per-repository winner: every pin file in every
directory declares its own scope, so a monorepo emits one set of facts per
package directory. A delta generation re-emits only the pin files it changed.
A deleted pin file leaves its earlier facts in place until the projection
(#8816) retracts per repository on generation change; this package emits no
materialization marker fact.

## Verification

```bash
cd go && go test -race ./internal/collector/repo/git/... -count=1
```

Changes to emitted facts also need the golden-corpus gate (B-7) and the B-12
snapshot; see `docs/public/reference/local-testing/golden-corpus-gate.md`.
