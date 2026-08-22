# gitcodeowners

## Purpose

`go/internal/collector/gitrepo/gitcodeowners` owns CODEOWNERS ownership facts.

## Where this fits

```
sync -> discover -> parse -> emit facts -> enqueue -> reducer -> projection -> query
                                    ^
                              this package
```

`gitrepo` drives the repository snapshot and the fact stream. It calls into this
package during emission; this package never calls back into `gitrepo`. Anything
both sides need lives in `go/internal/collector/gitrepo/gitmodel`.

## Exported surface

- `IsCodeownersCandidatePath`, `NoteCodeownersCandidate` — candidate detection
  during the content walk.
- `ResolvedCodeownersCandidateRelativePaths` — the single winning file per
  repository.
- `ExtractCodeownersCandidateFiles`, `CodeownersFileMetasForPaths` — candidate
  resolution to content records.
- `EmitCodeownersFactsForCandidates` — emission through the shared writer.

## Notes

The name is `gitcodeowners`, not `codeowners`: `collector/codeowners` already
exists and this package imports it.

CODEOWNERS may legally live in more than one directory. Exactly one wins per
repository, and `ResolvedCodeownersCandidateRelativePaths` is where that
precedence is decided — emitting from two locations would double-count
ownership.

## Verification

```bash
cd go && go test ./internal/collector/gitrepo/... -count=1
```

Changes to emitted facts also need the golden-corpus gate (B-7) and the B-12
snapshot; see `docs/public/reference/local-testing/golden-corpus-gate.md`.
