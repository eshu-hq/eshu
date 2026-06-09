# AGENTS.md - collector/imagepreflight guidance for LLM assistants

## Read first

1. `go/internal/collector/imagepreflight/README.md`
2. `go/internal/collector/imagepreflight/doc.go`
3. `go/internal/collector/imagepreflight/preflight.go`
4. `docs/internal/design/1737-visual-media-documentation-ingestion.md`
5. `go/internal/collector/README.md`

## Invariants

- Keep this package pure. No repository discovery, collector wiring, fact
  emission, storage calls, graph writes, API/MCP routes, goroutines, provider
  calls, OCR, model calls, renderer execution, network access, temporary files,
  or telemetry side effects belong here.
- Return metadata-only decisions. Do not persist raw pixels, OCR text, EXIF
  values, source names, local paths, private URLs, credentials, thumbnails,
  camera serials, user names, or image bytes.
- Preserve low-cardinality warning classes. Do not add raw image strings,
  source paths, URLs, user names, tenant names, or private IDs to warning
  fields.
- Treat a clean preflight as necessary but not sufficient for ingestion. Full
  image OCR still needs parser tests, fact readback proof, telemetry, ACL
  handling, dependency review, and security review.

## Common changes and how to scope them

- Add a new warning class with a focused test and update `README.md`.
- Add a resource limit by extending `Options`, defaulting it in
  `normalizeOptions`, and testing the over-limit class.
- Add OCR extraction outside this package. Extractors may call `Preflight`, but
  extraction and fact emission belong in their owning documentation collector
  slice.
- Add WebP decoding only after dependency review. This package intentionally
  reports WebP as `unsupported_codec` while it uses only the standard library.
- Add runtime telemetry only from a caller. This package should stay
  side-effect free.

## Failure modes

- Unknown extensions return `unsupported_format`.
- WebP returns `unsupported_codec`.
- Corrupt PNG, JPEG, or GIF inputs return `malformed_media`.
- Oversized sources or pixel limits return `resource_limit_exceeded`.
- Animated GIFs return `partial_extraction` to preserve first-frame-only
  behavior.
- Metadata-looking, sensitive-looking, and external-reference markers return
  explicit design-owned warning classes without returning the raw strings.
- Caller cancellation or deadline returns `timeout` and no partially trusted
  safe result.

## Anti-patterns

- Running OCR, calling a vision model, rendering pixels, or decoding thumbnails
  in preflight.
- Recording OCR text, raw pixels, EXIF values, source names, local paths,
  private URLs, user names, camera serials, or image bytes in result payloads.
- Treating image preflight as hosted ingestion approval.
- Adding fact emission, graph truth, API/MCP readback, runtime flags, or
  telemetry contracts without a separate security-reviewed implementation
  slice.
