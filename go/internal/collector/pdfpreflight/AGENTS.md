# AGENTS.md - collector/pdfpreflight guidance for LLM assistants

## Read first

1. `go/internal/collector/pdfpreflight/README.md`
2. `go/internal/collector/pdfpreflight/doc.go`
3. `go/internal/collector/pdfpreflight/preflight.go`
4. `docs/internal/design/1737-visual-media-documentation-ingestion.md`
5. `go/internal/collector/README.md`

## Invariants

- Keep this package pure. No repository discovery, collector wiring, fact
  emission, storage calls, graph writes, API/MCP routes, goroutines, provider
  calls, renderer execution, OCR, JavaScript execution, network access,
  temporary files, or telemetry side effects belong here.
- Return metadata-only decisions. Do not persist page text, links, titles,
  authors, metadata values, source names, local paths, private URLs,
  credentials, or secrets.
- Preserve low-cardinality warning classes. Do not add raw PDF strings, source
  paths, URLs, user names, tenant names, or private IDs to warning fields.
- Treat a clean preflight as necessary but not sufficient for ingestion. Full
  PDF extraction still needs parser tests, fact readback proof, telemetry, ACL
  handling, and security review.

## Common changes and how to scope them

- Add a new warning class with a focused test and update `README.md`.
- Add a resource limit by extending `Options`, defaulting it in
  `normalizeOptions`, and testing the over-limit class.
- Add PDF extraction outside this package. Extractors may call `Preflight`, but
  extraction and fact emission belong in their owning documentation collector
  slice.
- Add runtime telemetry only from a caller. This package should stay side-effect
  free.

## Failure modes

- Missing PDF header returns `malformed_pdf`.
- Missing trailer EOF returns `partial_extraction`.
- Encryption, JavaScript/action content, embedded files, annotations, external
  references, and metadata fields return explicit design-owned warning classes.
  Embedded-file markers are counted separately and reported under
  `unsupported_active_content`.
- Caller cancellation or deadline returns `timeout` and no partially trusted
  safe result.

## Anti-patterns

- Rendering pages, running OCR, executing JavaScript, or decoding object streams
  in preflight.
- Recording PDF text, links, titles, authors, metadata values, or source names
  in result payloads.
- Treating PDF preflight as hosted ingestion approval.
- Adding fact emission, graph truth, API/MCP readback, or runtime flags without
  a separate security-reviewed implementation slice.
