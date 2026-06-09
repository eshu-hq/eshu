# OOXML Preflight

## Purpose

`collector/ooxmlpreflight` classifies `.docx`, `.xlsx`, and `.pptx` package
safety before any Office-document extractor reads document text. It gives future
documentation collectors a metadata-only guard for resource limits, unsafe
paths, external relationships, macro-bearing parts, active content, embedded
objects, malformed package metadata, and bounded structure-marker counts.

## Ownership boundary

This package owns pure preflight classification for Office Open XML packages.
It reads ZIP directory metadata, bounded package metadata XML, and selected
structure-only XML start elements needed to count tables, tracked changes,
worksheets, formulas, hidden sheet/slide markers, slides, notes, comments, and
media parts. It does not persist document bodies, comments, authors, cell
values, formula bodies, speaker notes, image bytes, embedded object bytes,
local part names, or private relationship targets.

It does not discover repositories, emit documentation facts, persist rows,
write graph state, expose API or MCP routes, add runtime knobs, or enable
hosted/repository ingestion. Format-specific extractors and collector wiring
must remain separate follow-up work with security review.

## Exported surface

See `doc.go` for the godoc-rendered package contract.

- `Options` sets source-byte, expanded-byte, entry-count, compression-ratio,
  XML-byte, and XML-depth budgets.
- `Result` reports format, safe/unsafe state, bounded counts, and warning
  classes. Structure counters are metadata-only counts for annotation parts,
  hidden content markers, images, DOCX tables and tracked changes, XLSX
  worksheets/shared strings/formulas, and PPTX slides/notes/media.
- `Warning` records a stable low-cardinality class and count.
- `Preflight` classifies one package using an `io.ReaderAt`, source name, byte
  size, context, and options.
- Format constants: `FormatDOCX`, `FormatXLSX`, and `FormatPPTX`.
- Warning constants cover unsupported formats, macro-enabled packages,
  malformed containers/XML, resource limits, compression-ratio limits, unsafe
  paths, external relationships, active content, embedded objects, skipped
  annotation text, skipped hidden content, and cancellation/deadline timeout.
  Issue-specific warning classes `external_relationship`,
  `active_content_present`, `embedded_object_present`,
  `annotation_text_skipped`, and `hidden_content_skipped` are recorded in the
  #1738 design before collector use.

## Dependencies

The package uses only the Go standard library. `archive/zip` opens the package,
`encoding/xml` parses bounded content-type, relationship, and structure-marker
parts without storing character data, and `context` lets callers stop preflight
before extraction proceeds.

## Telemetry

None directly. This package has no runtime side effects and no metric, log, or
trace emission. Future collector integration must record bounded extraction
attempts, warning classes, skipped packages, bytes inspected, elapsed time, and
truncation/resource outcomes through collector telemetry before enabling an
extractor.

Collector Performance Evidence: `go test ./internal/collector/ooxmlpreflight
-count=1` proves package classification is bounded by source bytes, expanded
bytes, entry count, XML bytes, XML depth, and compression ratio. `go test
./internal/collector -run 'OOXML|DocumentationDefaultOff' -count=1` proves
`.docx`, `.xlsx`, and `.pptx` remain on the parser path instead of entering
documentation extraction by default.

Collector Observability Evidence: this package emits no facts, metrics, spans,
logs, status rows, or graph writes. Future extractor wiring must add runtime
collector signals for attempted package preflights, warning classes, skipped
packages, elapsed time, bytes inspected, and resource-limit outcomes before
enabling Office ingestion.

Collector Deployment Evidence: no Docker Compose, Helm, Service, ServiceMonitor,
collector binary, runtime flag, or environment-variable path changes in this
slice. The default-off collector routing test keeps Office packages out of
hosted documentation ingestion until a reviewed extractor slice enables them.

No-Observability-Change: this package is a pure metadata preflight helper. It
adds no worker, queue consumer, graph write, database query, HTTP handler, MCP
tool, runtime stage, or hosted collector path.

## Gotchas / invariants

- Preflight never stores raw relationship targets. External relationships are
  counted and classified, not persisted.
- Structure scanning counts XML tag/attribute markers only. It ignores
  character data and never returns comments, tracked-change text, sheet names,
  formulas, shared strings, speaker notes, authors, alt text, or part names.
- ZIP part paths must be local slash-separated names with no drive prefix,
  absolute path, backslash, `..` segment, empty component, or NUL byte.
- Macro-enabled extensions and macro-like package parts fail closed with
  `unsupported_macro_enabled`.
- Compression-ratio checks ignore very small parts to avoid noisy XML-header
  ratios; expanded-byte and entry-count budgets still bound the package.
- `annotation_text_skipped` and `hidden_content_skipped` are presence warnings;
  the dedicated structure counters carry the bounded counts.
- A safe preflight result is not approval to ingest. It only means this guard
  did not find a disqualifying package-level issue.

## Related docs

- `docs/internal/design/1738-office-spreadsheet-deck-archive-ingestion.md`
- `docs/public/reference/local-testing.md`
- Go `archive/zip`: https://pkg.go.dev/archive/zip
- Microsoft Open Packaging Conventions overview:
  https://learn.microsoft.com/en-us/previous-versions/windows/desktop/opc/open-packaging-conventions-overview
