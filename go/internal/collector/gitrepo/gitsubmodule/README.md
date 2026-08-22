# gitsubmodule

## Purpose

`go/internal/collector/gitrepo/gitsubmodule` owns `.gitmodules` submodule facts and pinned-SHA resolution.

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

- `IsGitmodulesCandidatePath`, `NoteSubmoduleCandidate` — candidate detection
  during the content walk.
- `ExtractGitmodulesCandidateFiles`, `GitmodulesFileMetasForPaths` — candidate
  resolution to content records.
- `EmitSubmoduleFactsForCandidates` — emission through the shared writer.
- `GitSubmoduleGitlinkSHA` — the pinned commit each submodule points at.

## Notes

The name is `gitsubmodule`, not `submodule`: `collector/submodule` already
exists and this package imports it. The dirgate naming rule would also flag a
`submodule`-named file sitting beside a `submodule` package.

There is exactly one recognized .gitmodules location, so emission happens once
after both content branches close.

## Verification

```bash
cd go && go test ./internal/collector/gitrepo/... -count=1
```

Changes to emitted facts also need the golden-corpus gate (B-7) and the B-12
snapshot; see `docs/public/reference/local-testing/golden-corpus-gate.md`.
