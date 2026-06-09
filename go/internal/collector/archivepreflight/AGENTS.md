# AGENTS.md - collector/archivepreflight guidance for LLM assistants

## Read first

1. `go/internal/collector/archivepreflight/README.md`
2. `go/internal/collector/archivepreflight/doc.go`
3. `go/internal/collector/archivepreflight/preflight.go`
4. `docs/internal/design/1738-office-spreadsheet-deck-archive-ingestion.md`
5. `go/internal/collector/README.md`

## Invariants

- Keep this package pure. No repository discovery, collector wiring, fact
  emission, storage calls, graph writes, API/MCP routes, goroutines, provider
  calls, filesystem extraction, or telemetry side effects belong here.
- Return metadata-only decisions. Do not persist member paths, source names,
  document text, embedded bytes, local paths, private URLs, credentials, or
  secrets.
- Preserve low-cardinality warning classes. Do not add raw archive member names,
  source paths, user names, tenant names, or private URLs to warning fields.
- Treat a clean preflight as necessary but not sufficient for ingestion. Full
  archive routing still needs contained-format tests, fact readback proof,
  telemetry, ACL handling, and security review.

## Common changes and how to scope them

- Add a new warning class with a focused test and update `README.md`.
- Add a resource limit by extending `Options`, defaulting it in
  `normalizeOptions`, and testing the over-limit class.
- Add archive extraction outside this package. Extractors may call `Preflight`,
  but extraction and fact emission belong in their owning documentation
  collector slice.
- Add runtime telemetry only from a caller. This package should stay side-effect
  free.

## Failure modes

- Malformed archives return `malformed_container` rather than an empty safe
  result.
- Unsafe paths return `archive_path_escape` even if Go's ZIP reader accepts the
  archive under the current `GODEBUG` setting.
- Symlinks, hardlinks, special files, nested archives, and credential-like
  members return explicit skip classes.
- Caller cancellation or deadline returns `timeout` and no partially trusted
  safe result.

## Anti-patterns

- Extracting archive members to disk in preflight.
- Recording member names or source names in result payloads.
- Treating archive preflight as hosted ingestion approval.
- Adding `.tar.gz`, `.tgz`, nested archive recursion, or contained-document
  routing without a separate security-reviewed implementation slice.
