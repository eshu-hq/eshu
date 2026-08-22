# gitdocs

## Purpose

`go/internal/collector/gitrepo/gitdocs` owns documentation extraction for the git collector.

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

- `GitDocumentationFormat` + `GitDocumentationSourceURIAndFormat` — format
  routing from a relative path.
- `IsGitDocumentationPath`, `IsNotebookDocumentationPath` — path predicates the
  snapshot partitioner uses.
- `DocumentationFileMetasForPaths` — metadata-only records for phase A.
- `EmitGitDocumentationFactsForContentFile`,
  `GitDocumentationEnvelopesForContentFile` — fact emission for phase B.
- `ReadDocumentationBody`, `DocumentationMetaRelativePaths`,
  `CleanDiagramLinkTarget` — helpers gitrepo still calls by name.

## Notes

Several size and shape limits are exported (`DocumentationMaxBodyBytes`,
`DocumentationMaxSectionChars`, `PptxMaxSlides`, `SpreadsheetMaxRows`,
`SpreadsheetSampleRows`, `SpreadsheetMaxColumns`) because the collector-root
tests that assert them stayed in gitrepo. They are budget constants, not
tuning knobs — changing one changes emitted fact volume and the B-12 snapshot.

Tests split by what they exercise: unit tests of format routing and extraction
live here, while tests that drive a whole generation through
`buildStreamingGeneration` stayed in gitrepo, because that is where the wiring
they assert lives.

## Verification

```bash
cd go && go test ./internal/collector/gitrepo/... -count=1
```

Changes to emitted facts also need the golden-corpus gate (B-7) and the B-12
snapshot; see `docs/public/reference/local-testing/golden-corpus-gate.md`.
