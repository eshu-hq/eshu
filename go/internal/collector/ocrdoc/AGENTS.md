# AGENTS.md - collector/ocrdoc guidance for LLM assistants

## Read first

1. `go/internal/collector/ocrdoc/README.md`
2. `go/internal/collector/ocrdoc/doc.go`
3. `go/internal/collector/ocrdoc/extract.go`
4. `go/internal/collector/imagepreflight/README.md`
5. `docs/internal/design/1737-visual-media-documentation-ingestion.md`

## Invariants

- Keep OCR default-off. Do not wire this package into hosted collectors,
  repository discovery, CLI flags, Helm values, or Compose profiles without a
  separate security-reviewed issue.
- Emit source facts only: `documentation_document` and `documentation_section`.
  Do not emit entity mentions, claim candidates, graph edges, service truth,
  deployment truth, incident truth, or ownership truth from OCR text.
- Run `imagepreflight` before calling an OCR engine. WebP stays unsupported
  until a decoder dependency review lands.
- Do not persist raw pixels, image bytes, EXIF values, local paths, private
  URLs, credentials, usernames, camera serials, OCR intermediates, or sensitive
  OCR text.
- Warning classes must stay low-cardinality and design-owned.

## Common changes and how to scope them

- Add an OCR engine behavior by extending the injected `Engine` contract and
  testing it with synthetic fixtures.
- Add redaction classes by updating extractor tests, the README, and the
  redaction helper. Keep raw matching values out of persisted content.
- Add runtime telemetry only in the caller that enables extraction. This package
  has no metric, span, log, or status side effects.

## Failure modes

- Unsupported codecs, malformed media, resource limits, and canceled preflight
  emit a skipped document fact with warning metadata and no section facts.
- Safe images with no OCR regions emit a document fact with `ocr_status=no_text`.
- Multi-frame GIF input emits first-frame OCR sections and keeps
  `partial_extraction` metadata.
- Sensitive OCR regions persist redacted content with hashes and redaction
  metadata.

## Anti-patterns

- Adding cloud OCR, vision models, network fetches, renderer execution, or temp
  file handling here.
- Treating OCR text as canonical service, deployment, owner, incident, or graph
  truth.
- Hiding malformed or unsupported media as an empty successful document.
- Adding route, MCP, graph, reducer, or storage logic to this package.

## What not to change without review

- Runtime enablement, sandbox settings, OCR dependencies, WebP decoding,
  all-frame GIF OCR, raw excerpt return policy, or any graph/reducer
  materialization path require a separate reviewed design update.
