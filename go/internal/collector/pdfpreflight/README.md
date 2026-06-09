# PDF Preflight

## Purpose

`collector/pdfpreflight` classifies PDF documentation sources before any PDF
extractor reads page text, links, or document metadata. It gives future
documentation collectors a metadata-only guard for resource limits, malformed
files, encryption, active content, embedded files, external references,
annotations, metadata redaction, and image-only signals.

## Ownership boundary

This package owns pure preflight classification for `.pdf` files. It reads
bounded source bytes only to classify PDF markers and warning counts. It does
not persist page text, links, titles, authors, metadata strings, source names,
private URLs, object streams, or document content.

It does not discover repositories, emit documentation facts, persist rows, write
graph state, expose API or MCP routes, add runtime knobs, execute JavaScript,
render pages, run OCR, fetch network resources, or enable hosted or repository
ingestion. PDF extraction, fact emission, ACL behavior, and security-review
enablement belong in separate follow-up slices.

## Exported surface

See `doc.go` for the godoc-rendered package contract.

- `Options` sets the source-byte budget.
- `Result` reports format, safe/unsafe state, bounded counts, and warning
classes.
- `Warning` records a stable low-cardinality class and count.
- `Preflight` classifies one PDF using an `io.ReaderAt`, source name, byte
  size, context, and options.
- Format constants cover PDF.
- Warning constants cover unsupported format, malformed PDF, encrypted PDFs,
  scanned/image-only signals, active content, resource limits, timeout,
  external references, skipped annotation text, metadata redaction, and partial
  extraction. Embedded-file markers are counted separately but classified under
  `unsupported_active_content`, matching the visual-media design failure
  classes.

## Dependencies

The package uses only the Go standard library. It performs bounded byte scans
for PDF marker strings and does not use a PDF parser, renderer, OCR engine,
JavaScript runtime, temporary files, or network access.

## Telemetry

None directly. This package has no runtime side effects and no metric, log, or
trace emission. Future PDF collector integration must record bounded extraction
attempts, warning classes, bytes inspected, elapsed time, skipped active
content, skipped annotations, metadata redaction, and resource outcomes through
collector telemetry before enabling PDF ingestion.

Collector Performance Evidence: `go test ./internal/collector/pdfpreflight
-count=1` proves PDF preflight is bounded by source bytes and marker-only
classification. `go test ./internal/collector -run
'PDF|DocumentationDefaultOff' -count=1` proves `.pdf` remains outside
documentation extraction by default.

Collector Observability Evidence: this package emits no facts, metrics, spans,
logs, status rows, or graph writes. Future extractor wiring must add runtime
collector signals for attempted PDF preflights, warning classes, elapsed time,
bytes inspected, page/object counts, and resource-limit outcomes before enabling
PDF ingestion.

Collector Deployment Evidence: no Docker Compose, Helm, Service, ServiceMonitor,
collector binary, runtime flag, or environment-variable path changes in this
slice. The default-off collector routing test keeps PDF files out of hosted
documentation ingestion until a reviewed extractor slice enables them.

No-Observability-Change: this package is a pure metadata preflight helper. It
adds no worker, queue consumer, graph write, database query, HTTP handler, MCP
tool, runtime stage, or hosted collector path.

## Gotchas / invariants

- Preflight never stores raw PDF text, links, metadata values, source names, or
  object contents in `Result`.
- Encrypted PDFs, active content, embedded files, annotations, and metadata
  fields fail closed with design-owned warning classes.
- Image-only detection is a cheap marker heuristic, not OCR. It only protects
  the first default-off guard and is not a claim that text extraction would
  succeed or fail.
- A safe preflight result is not approval to ingest. It only means this guard
  did not find a disqualifying package-level issue.

## Related docs

- `docs/internal/design/1737-visual-media-documentation-ingestion.md`
- `docs/public/reference/local-testing.md`
