# Media Preflight

## Purpose

`collector/mediapreflight` classifies audio and video documentation sources
before any transcript extractor reads audio samples, video frames, subtitles,
speaker labels, or transcript text. It gives future documentation collectors a
metadata-only guard for resource limits, malformed media, unsupported codecs,
no-audio markers, external references, sensitive-looking markers, and metadata
redaction.

## Ownership boundary

This package owns pure preflight classification for `.mp3`, `.wav`, `.m4a`,
`.ogg`, `.mp4`, `.mov`, `.webm`, and `.mkv` media sources. It reads bounded
source bytes only to decode safe WAV container metadata and classify warning
counts. It does not persist raw media bytes, transcript text, subtitle text,
speaker names, source names, local paths, private URLs, metadata values, audio
samples, or video frames.

It does not discover repositories, emit documentation facts, persist rows, write
graph state, expose API or MCP routes, add runtime knobs, run transcription,
execute codecs, trust subtitles, fetch network resources, create temporary
files, or enable hosted or repository ingestion. Transcription, fact emission,
ACL behavior, codec dependency review, and security-review enablement belong in
separate follow-up slices.

## Exported surface

See `doc.go` for the godoc-rendered package contract.

- `Options` sets source-byte and duration budgets.
- `Result` reports format, safe/unsafe state, duration, audio stream count,
  bounded marker counts, and warning classes.
- `Warning` records a stable low-cardinality class and count.
- `Preflight` classifies one media source using an `io.ReaderAt`, source name,
  byte size, context, and options.
- Format constants cover MP3, WAV, M4A, Ogg, MP4, MOV, WebM, and MKV.
- Warning constants cover unsupported format, unsupported codec, malformed
  media, resource limits, timeout, transcript-no-speech, external references,
  sensitive value redaction, and metadata redaction.

## Dependencies

The package uses only the Go standard library. It decodes WAV metadata from
RIFF chunks and reports other supported media extensions as `unsupported_codec`
until a reviewed codec/container dependency exists. It does not use media
decoders, transcription libraries, model runtimes, temporary files, or network
access.

## Telemetry

None directly. This package has no runtime side effects and no metric, log, or
trace emission. Future media collector integration must record bounded
extraction attempts, warning classes, bytes inspected, duration, codec/container
outcomes, elapsed time, skipped external references, metadata redaction, and
resource outcomes through collector telemetry before enabling transcription
ingestion.

Collector Performance Evidence: `go test ./internal/collector/mediapreflight
-count=1` proves media preflight is bounded by source bytes, duration limits,
and metadata-only classification. `go test ./internal/collector -run
'Media|DocumentationDefaultOff' -count=1` proves media formats remain outside
documentation extraction by default.

Collector Observability Evidence: this package emits no facts, metrics, spans,
logs, status rows, or graph writes. Future transcription wiring must add
runtime collector signals for attempted media preflights, warning classes,
elapsed time, bytes inspected, media duration, codec/container outcomes, and
resource-limit outcomes before enabling media ingestion.

Collector Deployment Evidence: no Docker Compose, Helm, Service,
ServiceMonitor, collector binary, runtime flag, or environment-variable path
changes in this slice. The default-off collector routing test keeps media files
out of hosted documentation ingestion until a reviewed transcription extractor
slice enables them.

No-Observability-Change: this package is a pure metadata preflight helper. It
adds no worker, queue consumer, graph write, database query, HTTP handler, MCP
tool, runtime stage, transcription engine, codec runtime, or hosted collector
path.

## Gotchas / invariants

- Preflight never stores media bytes, transcript text, subtitle text, speaker
  names, source names, local paths, private URLs, metadata values, audio
  samples, or video frames in `Result`.
- Non-WAV media extensions are recognized but classified as `unsupported_codec`
  until a reviewed parser/prober dependency exists.
- No-audio markers are metadata-only warnings. They are not proof that a later
  transcription engine heard no speech.
- Sensitive-looking or metadata-looking markers are counted only as warning
  classes. They are not returned as strings.
- A safe preflight result is not approval to ingest. It only means this guard
  did not find a disqualifying package-level issue.

## Related docs

- `docs/internal/design/1737-visual-media-documentation-ingestion.md`
- `docs/public/reference/local-testing.md`
