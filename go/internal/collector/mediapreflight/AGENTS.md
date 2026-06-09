# AGENTS.md - collector/mediapreflight guidance for LLM assistants

## Read first

1. `go/internal/collector/mediapreflight/README.md`
2. `go/internal/collector/mediapreflight/doc.go`
3. `go/internal/collector/mediapreflight/preflight.go`
4. `docs/internal/design/1737-visual-media-documentation-ingestion.md`
5. `go/internal/collector/README.md`

## Invariants

- Keep this package pure. No repository discovery, collector wiring, fact
  emission, storage calls, graph writes, API/MCP routes, goroutines, provider
  calls, transcription, codec execution, subtitle trust, network access,
  temporary files, or telemetry side effects belong here.
- Return metadata-only decisions. Do not persist media bytes, transcript text,
  subtitle text, speaker names, source names, local paths, private URLs,
  credentials, user names, audio samples, or video frames.
- Preserve low-cardinality warning classes. Do not add raw media strings,
  source paths, URLs, user names, tenant names, or private IDs to warning
  fields.
- Treat a clean preflight as necessary but not sufficient for ingestion. Full
  transcription still needs parser tests, fact readback proof, telemetry, ACL
  handling, dependency review, and security review.

## Common changes and how to scope them

- Add a new warning class with a focused test and update `README.md`.
- Add a resource limit by extending `Options`, defaulting it in
  `normalizeOptions`, and testing the over-limit class.
- Add media parsing or transcription outside this package. Extractors may call
  `Preflight`, but extraction and fact emission belong in their owning
  documentation collector slice.
- Add codec/container decoding only after dependency review. This package
  intentionally reports non-WAV media as `unsupported_codec` while it uses only
  the standard library.
- Add runtime telemetry only from a caller. This package should stay
  side-effect free.

## Failure modes

- Unknown extensions return `unsupported_format`.
- Non-WAV supported media extensions return `unsupported_codec`.
- Corrupt WAV inputs return `malformed_media`.
- Oversized sources or duration limits return `resource_limit_exceeded`.
- Metadata-only no-audio markers return `transcript_no_speech`.
- Metadata-looking, sensitive-looking, and external-reference markers return
  explicit design-owned warning classes without returning the raw strings.
- Caller cancellation or deadline returns `timeout` and no partially trusted
  safe result.

## Anti-patterns

- Running transcription, executing codecs, trusting subtitle text, reading video
  frames, or decoding audio samples in preflight.
- Recording transcript text, subtitle text, media bytes, speaker names, source
  names, local paths, private URLs, user names, audio samples, or video frames
  in result payloads.
- Treating media preflight as hosted ingestion approval.
- Adding fact emission, graph truth, API/MCP readback, runtime flags, or
  telemetry contracts without a separate security-reviewed implementation
  slice.
