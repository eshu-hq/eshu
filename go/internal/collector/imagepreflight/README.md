# Image Preflight

## Purpose

`collector/imagepreflight` classifies image and screenshot documentation
sources before any OCR extractor reads pixels, visible text, EXIF metadata, or
image-derived summaries. It gives future documentation collectors a
metadata-only guard for resource limits, malformed media, unsupported codecs,
animated GIF first-frame handling, external references, sensitive-looking
markers, and metadata redaction.

## Ownership boundary

This package owns pure preflight classification for `.png`, `.jpg`, `.jpeg`,
`.gif`, and `.webp` image sources. It reads bounded source bytes only to decode
container metadata and classify warning counts. It does not persist raw pixels,
OCR text, EXIF values, source names, local paths, private URLs, thumbnails,
camera metadata, or image bytes.

It does not discover repositories, emit documentation facts, persist rows, write
graph state, expose API or MCP routes, add runtime knobs, run OCR, call vision
models, fetch network resources, create temporary files, or enable hosted or
repository ingestion. OCR extraction, fact emission, ACL behavior, dependency
review, and security-review enablement belong in separate follow-up slices.

## Exported surface

See `doc.go` for the godoc-rendered package contract.

- `Options` sets source-byte and pixel-count budgets.
- `Result` reports format, safe/unsafe state, dimensions, frame count, bounded
  marker counts, and warning classes.
- `Warning` records a stable low-cardinality class and count.
- `Preflight` classifies one image using an `io.ReaderAt`, source name, byte
  size, context, and options.
- Format constants cover PNG, JPEG, GIF, and WebP. WebP currently reports
  `unsupported_codec` because this package avoids adding a decoder dependency.
- Warning constants cover unsupported format, unsupported codec, malformed
  media, resource limits, timeout, external references, sensitive value
  redaction, metadata redaction, and partial extraction.

## Dependencies

The package uses only the Go standard library. It decodes PNG, JPEG, and GIF
container metadata through `image.DecodeConfig`, counts GIF frames from the
container structure, and does not use OCR libraries, image renderers, vision
models, temporary files, or network access.

## Telemetry

None directly. This package has no runtime side effects and no metric, log, or
trace emission. Future image collector integration must record bounded
extraction attempts, warning classes, bytes inspected, decoded dimensions,
frame counts, elapsed time, skipped external references, metadata redaction,
and resource outcomes through collector telemetry before enabling OCR
ingestion.

Collector Performance Evidence: `go test ./internal/collector/imagepreflight
-count=1` proves image preflight is bounded by source bytes, pixel limits, and
metadata-only classification. `go test ./internal/collector -run
'Image|DocumentationDefaultOff' -count=1` proves image formats remain outside
documentation extraction by default.

Collector Observability Evidence: this package emits no facts, metrics, spans,
logs, status rows, or graph writes. Future OCR wiring must add runtime
collector signals for attempted image preflights, warning classes, elapsed
time, bytes inspected, dimensions, frame counts, and resource-limit outcomes
before enabling image ingestion.

Collector Deployment Evidence: no Docker Compose, Helm, Service,
ServiceMonitor, collector binary, runtime flag, or environment-variable path
changes in this slice. The default-off collector routing test keeps image files
out of hosted documentation ingestion until a reviewed OCR extractor slice
enables them.

No-Observability-Change: this package is a pure metadata preflight helper. It
adds no worker, queue consumer, graph write, database query, HTTP handler, MCP
tool, runtime stage, OCR engine, or hosted collector path.

## Gotchas / invariants

- Preflight never stores raw image bytes, OCR text, EXIF values, source names,
  local paths, private URLs, or metadata values in `Result`.
- WebP is recognized by extension but classified as `unsupported_codec` until a
  reviewed decoder dependency exists.
- Animated GIFs fail closed with `partial_extraction`; the first reviewed OCR
  slice may inspect only one frame unless a later design raises that boundary.
- Sensitive-looking or metadata-looking markers are counted only as warning
  classes. They are not returned as strings.
- A safe preflight result is not approval to ingest. It only means this guard
  did not find a disqualifying package-level issue.

## Related docs

- `docs/internal/design/1737-visual-media-documentation-ingestion.md`
- `docs/public/reference/local-testing.md`
