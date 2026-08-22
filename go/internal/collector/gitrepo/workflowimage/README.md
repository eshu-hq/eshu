# workflowimage

## Purpose

`go/internal/collector/gitrepo/workflowimage` owns container image evidence from CI workflow files.

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

- `CurrentWorkflowImageFileMetas` — workflow files worth reading this run.
- `EmitWorkflowImageEvidenceFactsForContentFile` — emission through the shared
  writer.

## Notes

Only `.github/workflows` paths qualify. Widening that predicate changes which
files produce evidence facts and therefore the B-12 snapshot; re-run the
golden-corpus gate if you touch it.

## Verification

```bash
cd go && go test ./internal/collector/gitrepo/... -count=1
```

Changes to emitted facts also need the golden-corpus gate (B-7) and the B-12
snapshot; see `docs/public/reference/local-testing/golden-corpus-gate.md`.
