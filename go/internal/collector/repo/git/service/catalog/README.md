# catalog

## Purpose

`go/internal/collector/repo/git/service/catalog` owns service catalog manifest facts.

## Where this fits

```
sync -> discover -> parse -> emit facts -> enqueue -> reducer -> projection -> query
                                    ^
                              this package
```

`git` drives the repository snapshot and the fact stream. It calls into this
package during emission; this package never calls back into `git`. Anything
both sides need lives in `go/internal/collector/repo/git/model`.

## Exported surface

- `ServiceCatalogProviderForPath` — provider detection from a relative path.
- `EmitServiceCatalogFactsForContentFile` — emission through the shared
  writer.

## Notes

The name is `catalog`, not `servicecatalog`: `collector/servicecatalog`
already exists and this package imports it.

Its tests stayed in git. They assert catalog facts by draining a whole
generation through `buildStreamingGeneration`, which is git wiring, not
anything this package owns.

## Verification

```bash
cd go && go test ./internal/collector/repo/git/... -count=1
```

Changes to emitted facts also need the golden-corpus gate (B-7) and the B-12
snapshot; see `docs/public/reference/local-testing/golden-corpus-gate.md`.
