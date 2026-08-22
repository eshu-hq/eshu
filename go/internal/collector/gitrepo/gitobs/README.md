# gitobs

## Purpose

`go/internal/collector/gitrepo/gitobs` owns observability route and metric facts from repository config files.

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

- `ObservabilityFactCount` — the pre-count the fact stream uses to size its
  generation estimate.
- `EmitObservabilityFactsForFile` — per-file emission through the shared
  writer.

## Notes

`commitSHAByRelativePath` used to live in this file and does not any more. It
reads `RepositorySnapshot`, so leaving it here would have forced this package
to import gitrepo while gitrepo imports this package — a cycle. It now sits in
`gitrepo/git_snapshot_commit_index.go`, next to the type it reads.

## Verification

```bash
cd go && go test ./internal/collector/gitrepo/... -count=1
```

Changes to emitted facts also need the golden-corpus gate (B-7) and the B-12
snapshot; see `docs/public/reference/local-testing/golden-corpus-gate.md`.
