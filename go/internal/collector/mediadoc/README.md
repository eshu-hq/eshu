# Media Transcript Documentation

## Purpose

`collector/mediadoc` turns reviewed local transcript output for media
documentation artifacts into source-neutral documentation document and section
facts. It exists so audio/video transcript facts can be tested and read back
without enabling a hosted collector path or treating spoken text as operational
truth.

## Ownership boundary

This package owns the post-preflight transcript fact boundary for local media
inputs. It calls `mediapreflight` first, invokes only an injected transcript
engine for supported inputs, redacts sensitive-looking transcript segments, and
emits `documentation_document` plus timestamped `documentation_section`
envelopes.

It does not discover repositories, add runtime flags, create temp files, call
cloud transcription APIs, persist raw media bytes, read video frames, persist
speaker names, trust subtitles, emit graph edges, infer service or deployment
truth, create documentation mentions, or create claim candidates. Runtime
enablement still requires sandbox, dependency, telemetry, ACL, and security
review.

## Exported surface

See `doc.go` for the godoc-rendered package contract.

- `Engine` is the injected local transcript boundary.
- `Request` describes one media artifact revision and durable scope metadata.
- `Options` forwards media preflight limits and section text bounds.
- `Media`, `EngineResult`, and `Segment` describe bounded transcript input and
  output without source-specific types.
- `Extract` runs preflight, calls the engine when allowed, and returns
  ready-to-persist fact envelopes.

## Dependencies

The package depends on `internal/collector/mediapreflight` for metadata-only
media gating, `internal/facts` for documentation payload and envelope
contracts, and `internal/scope` for the documentation collector kind. It uses
only the Go standard library otherwise.

## Telemetry

None directly. This is a default-off helper with no runtime side effects. A
future runtime integration must add bounded extraction attempt metrics, warning
class counts, elapsed time, source bytes, duration, codec/container outcomes,
segment counts, redaction counts, logs, spans, and status evidence before
enablement.

Collector Performance Evidence: `go test ./internal/collector/mediadoc
./internal/collector/mediapreflight -count=1` proves transcript fact
construction is bounded by media preflight limits, synthetic transcript
segments, and section text bounds without adding discovery, queue, graph, or
runtime work.

Collector Observability Evidence: this package emits no facts outside the
returned envelopes and has no runtime side effects. Future runtime wiring must
add collector signals for attempted media preflight and transcription,
warning classes, elapsed time, bytes inspected, media duration, codec/container
outcomes, transcript segment counts, redaction counts, and resource outcomes
before enabling media ingestion.

Collector Deployment Evidence: no Docker Compose, Helm, Service,
ServiceMonitor, collector binary, runtime flag, environment-variable path, or
hosted collector route changes in this slice. Media transcripts remain callable
only through an injected engine in tests or future reviewed code.

No-Observability-Change: this package adds no worker, queue consumer, database
query, graph write, HTTP handler, MCP tool, runtime flag, hosted collector path,
metric, span, log, or status row. Existing collector, fact-store, HTTP, and MCP
readback surfaces already carry the emitted documentation facts.

## Gotchas / invariants

- Non-WAV media stays unsupported until a codec/container dependency review
  lands.
- Source identity fields are redacted when they contain scheme-bearing URIs,
  private URLs, hostnames, or embedded local paths. Keep raw locations out of
  document IDs, external IDs, canonical URIs, source URIs, and source record
  IDs.
- Speaker labels are never persisted as raw names. Sections may record only a
  presence marker and stable hash.
- Sensitive-looking transcript text is not persisted as content. The section
  keeps redaction metadata and stable hashes instead.
- Transcript text remains document evidence. This package must not emit entity
  mentions, claim candidates, graph relationships, ownership truth, deployment
  truth, incident truth, or service truth.
- A caller must provide a transcript engine; there is no default engine or
  hosted provider fallback.

## Related docs

- `docs/internal/design/1737-visual-media-documentation-ingestion.md`
- `go/internal/collector/mediapreflight/README.md`
- `docs/public/reference/local-testing.md`
