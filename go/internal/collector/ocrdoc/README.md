# OCR Documentation

## Purpose

`collector/ocrdoc` turns reviewed local OCR output for image documentation
artifacts into source-neutral documentation document and section facts. It
exists so image OCR can be tested and read back without enabling a hosted
collector path or treating screenshot text as operational truth.

## Ownership boundary

This package owns the post-preflight OCR fact boundary for `.png`, `.jpg`,
`.jpeg`, and first-frame `.gif` inputs. It calls `imagepreflight` first, invokes
only an injected OCR engine for supported inputs, redacts sensitive-looking OCR
regions, and emits `documentation_document` plus OCR-region
`documentation_section` envelopes.

It does not discover repositories, add runtime flags, create temp files, call
cloud OCR or vision APIs, persist raw pixels, emit graph edges, infer service or
deployment truth, create documentation mentions, or create claim candidates.
Runtime enablement still requires sandbox, dependency, telemetry, ACL, and
security review.

## Exported surface

See `doc.go` for the godoc-rendered package contract.

- `Engine` is the injected local OCR boundary.
- `Request` describes one image artifact revision and durable scope metadata.
- `Options` forwards image preflight limits and section text bounds.
- `Image`, `EngineResult`, `Region`, and `Bounds` describe bounded OCR input and
  output without source-specific types.
- `Extract` runs preflight, calls the engine when allowed, and returns
  ready-to-persist fact envelopes.

## Dependencies

The package depends on `internal/collector/imagepreflight` for metadata-only
image gating, `internal/facts` for documentation payload and envelope contracts,
and `internal/scope` for the documentation collector kind. It uses only the Go
standard library otherwise.

## Telemetry

None directly. This is a default-off helper with no runtime side effects. A
future runtime integration must add bounded extraction attempt metrics, warning
class counts, elapsed time, source bytes, image dimensions, OCR region counts,
redaction counts, logs, spans, and status evidence before enablement.

Collector Performance Evidence: `go test ./internal/collector/ocrdoc
./internal/collector/imagepreflight -count=1` proves OCR fact construction is
bounded by image preflight limits, synthetic OCR regions, and section text
bounds without adding discovery, queue, graph, or runtime work.

Collector Observability Evidence: this package emits no facts outside the
returned envelopes and has no runtime side effects. Future runtime wiring must
add collector signals for attempted OCR extraction, warning classes, elapsed
time, bytes inspected, image dimensions, frame counts, OCR region counts,
redaction counts, and resource outcomes before enabling image ingestion.

Collector Deployment Evidence: no Docker Compose, Helm, Service,
ServiceMonitor, collector binary, runtime flag, environment-variable path, or
hosted collector route changes in this slice. OCR remains callable only through
an injected engine in tests or future reviewed code.

No-Observability-Change: this package adds no worker, queue consumer, database
query, graph write, HTTP handler, MCP tool, runtime flag, hosted collector path,
metric, span, log, or status row. Existing collector, fact-store, HTTP, and MCP
readback surfaces already carry the emitted documentation facts.

## Gotchas / invariants

- WebP remains unsupported unless a decoder dependency review lands first.
- Multi-frame GIF input is OCRed as frame zero only and keeps the
  `partial_extraction` warning.
- Sensitive-looking OCR text is not persisted as content. The section keeps
  redaction metadata and stable hashes instead.
- OCR text remains document evidence. This package must not emit entity
  mentions, claim candidates, graph relationships, ownership truth, deployment
  truth, incident truth, or service truth.
- A caller must provide an OCR engine; there is no default engine or hosted
  provider fallback.

## Related docs

- `docs/internal/design/1737-visual-media-documentation-ingestion.md`
- `go/internal/collector/imagepreflight/README.md`
- `docs/public/reference/local-testing.md`
