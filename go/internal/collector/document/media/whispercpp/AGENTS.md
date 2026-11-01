# AGENTS.md - media/whispercpp guidance for LLM assistants

## Read first

1. `go/internal/collector/document/media/whispercpp/README.md` (backend
   decision record, sandbox profile, packaging status)
2. `go/internal/collector/document/media/whispercpp/doc.go`
3. `go/internal/collector/document/media/whispercpp/engine.go` and
   `process.go`
4. `go/internal/collector/document/media/AGENTS.md`
5. `docs/internal/design/1737-visual-media-documentation-ingestion.md`

## Invariants

- Execute the engine only through `runProcess`: no shell, empty
  environment, fixed argument vector built in `New`, stdin input, stdout
  output, no temporary files. No argument may derive from media content,
  source names, or caller strings other than the validated language code.
- Keep decoding deterministic: greedy (`-bs 1 -bo 1`), `-tp 0`, `-nf`, fixed
  threads, fixed language. Do not enable language auto-detection, beam
  search, VAD, diarization, or grammar sampling without a decision update.
- Verify binary and model sha256 pins in `New`. Never add an unpinned path.
- Check every input cap before exec. Cap breaches, timeouts, cancellation,
  bad exits, and malformed output fail closed with a classified `*Error`.
- Never put transcript text, stderr, paths, or source names in error
  strings, span attributes, metric labels, or logs.
- Redaction belongs to `media.Extract`. Do not add a second redaction path
  here, and do not bypass Extract when persisting facts.
- No caller wiring, runtime flags, Helm values, or Compose profiles in this
  package. Enablement stays default-off and QA-tier only.

## Common changes and how to scope them

- Upgrading whisper.cpp or the model: update the pins in the README, the
  proof Dockerfile, and the proof script together, rerun
  `scripts/verify-media-transcript-sandbox.sh`, and record new evidence.
- Adding a media format: needs a reviewed decoder decision (see README
  "Formats"). Keep the in-process normalizer as the only path that hands
  bytes to whisper.cpp.
- Telemetry changes: update
  `go/internal/telemetry/instruments_media_transcript.go`,
  `docs/public/observability/telemetry-coverage.md`, and the README together.

## Failure modes

- `unsupported_format`, `malformed_media`, and input-side
  `resource_limit_exceeded` return before exec.
- `timeout`, `canceled`, output-side `resource_limit_exceeded`, and
  `engine_failed` kill the process group.
- `malformed_output` covers invalid JSON or inverted offsets.
- `no_speech` wraps `media.ErrNoSpeech`; Extract records it as a document
  warning, not an extraction error.

## Anti-patterns

- Linking whisper.cpp in process through cgo.
- Passing caller media bytes straight to whisper.cpp's own decoders.
- Cloud or hosted transcription fallbacks.
- Logging stderr "for debugging".
