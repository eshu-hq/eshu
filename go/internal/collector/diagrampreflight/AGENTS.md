# AGENTS.md - collector/diagrampreflight guidance for LLM assistants

## Read first

1. `go/internal/collector/diagrampreflight/README.md`
2. `go/internal/collector/diagrampreflight/doc.go`
3. `go/internal/collector/diagrampreflight/preflight.go`
4. `docs/internal/design/1737-visual-media-documentation-ingestion.md`
5. `go/internal/collector/README.md`

## Invariants

- Keep this package pure. No repository discovery, collector wiring, fact
  emission, storage calls, graph writes, API/MCP routes, goroutines, provider
  calls, renderer execution, preprocessor execution, network access, or
  telemetry side effects belong here.
- Return metadata-only decisions. Do not persist diagram text, labels, URLs,
  source names, include paths, local paths, private hostnames, credentials, or
  secrets.
- Preserve low-cardinality warning classes. Do not add raw diagram strings,
  source paths, URLs, user names, tenant names, or private IDs to warning
  fields.
- Treat a clean preflight as necessary but not sufficient for ingestion. Full
  diagram extraction still needs parser tests, fact readback proof, telemetry,
  ACL handling, and security review.

## Common changes and how to scope them

- Add a new warning class with a focused test and update `README.md`.
- Add a resource limit by extending `Options`, defaulting it in
  `normalizeOptions`, and testing the over-limit class.
- Add diagram extraction outside this package. Extractors may call `Preflight`,
  but extraction and fact emission belong in their owning documentation
  collector slice.
- Add runtime telemetry only from a caller. This package should stay side-effect
  free.

## Failure modes

- Malformed SVG/draw.io returns `malformed_xml`.
- Malformed Excalidraw returns `malformed_json`.
- Include directives, external entities, and external references return explicit
  skip or unsupported classes.
- Caller cancellation or deadline returns `timeout` and no partially trusted
  safe result.

## Anti-patterns

- Rendering diagrams, running PlantUML preprocessors, or executing SVG/script
  content in preflight.
- Recording diagram labels, URLs, include paths, or source names in result
  payloads.
- Treating diagram preflight as hosted ingestion approval.
- Adding fact emission, graph truth, API/MCP readback, or runtime flags without
  a separate security-reviewed implementation slice.
