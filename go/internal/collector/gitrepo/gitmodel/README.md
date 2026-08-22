# gitmodel

## Purpose

`go/internal/collector/gitrepo/gitmodel` owns the data the git collector passes between its own subpackages.

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

- `ContentFileSnapshot`, `ContentFileMeta`, `ContentEntitySnapshot` — the
  portable content records the two-phase snapshot passes between stages.
- `FactStreamWriter` + `NewFactStreamWriter` — the bounded fact channel every
  emitter writes through.
- `FactEnvelope`, `RepositoryRelativePath`, `PayloadString`, `PayloadPath`,
  `FirstNonEmptyString`, `DocumentationDigestForFile` — shared helpers.

## Notes

`FactStreamWriter`'s fields stay unexported and `NewFactStreamWriter` is the
only way to build one. `Send` increments the counter on every envelope, so a
literal with a nil counter would panic on the first fact; the constructor is
what makes that unrepresentable across the package boundary.

The `crypto/sha1` import carries a `#nosec G505` suppression. The digest is a
content-dedup key for documentation files, not a security primitive. That
annotation travelled with the function from the collector root — do not drop
it.

## Verification

```bash
cd go && go test ./internal/collector/gitrepo/... -count=1
```

Changes to emitted facts also need the golden-corpus gate (B-7) and the B-12
snapshot; see `docs/public/reference/local-testing/golden-corpus-gate.md`.
