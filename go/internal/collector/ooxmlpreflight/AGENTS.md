# AGENTS.md - collector/ooxmlpreflight guidance for LLM assistants

## Read first

1. `go/internal/collector/ooxmlpreflight/README.md`
2. `go/internal/collector/ooxmlpreflight/doc.go`
3. `go/internal/collector/ooxmlpreflight/preflight.go`
4. `docs/internal/design/1738-office-spreadsheet-deck-archive-ingestion.md`
5. `go/internal/collector/README.md`

## Invariants

- Keep this package pure. No repository discovery, collector wiring, fact
  emission, storage calls, graph writes, API/MCP routes, goroutines, provider
  calls, or telemetry side effects belong here.
- Return metadata-only decisions. Do not persist document text, spreadsheet
  values, slide notes, comments, authors, private relationship targets, local
  paths, embedded object bytes, image bytes, credentials, or secrets.
- Preserve low-cardinality warning classes. Do not add raw ZIP part names,
  relationship targets, source paths, user names, tenant names, or private URLs
  to warning fields.
- Treat a clean preflight as necessary but not sufficient for ingestion. Full
  extractors still need format-specific tests, fact readback proof, telemetry,
  and security review.

## Common changes and how to scope them

- Add a new warning class with a focused test and update `README.md`.
- Add a resource limit by extending `Options`, defaulting it in
  `normalizeOptions`, and testing the over-limit class.
- Add format-specific extraction outside this package. Extractors may call
  `Preflight`, but extraction and fact emission belong in their owning
  documentation collector slice.
- Add runtime telemetry only from a caller. This package should stay side-effect
  free.

## Failure modes

- Malformed ZIP containers return `malformed_container` rather than an empty
  safe result.
- Malformed content-type or relationship XML returns `malformed_xml`.
- Unsafe paths return `archive_path_escape` even if Go's ZIP reader accepts the
  archive under the current `GODEBUG` setting.
- Caller cancellation or deadline returns `timeout` and no partially trusted
  safe result.

## Anti-patterns

- Reading full document XML, comments, workbook cells, slide notes, or embedded
  object bytes in preflight.
- Recording relationship target strings or package part names in result
  payloads.
- Treating preflight as hosted ingestion approval.
- Adding `.docm`, `.xlsm`, `.pptm`, ActiveX, VBA, OLE, or embedded-object
  support without a separate security review.
