# DTMF Transcript Engine

## Purpose

`dtmf` is the local deterministic transcript backend behind the media
`Engine` contract. It decodes Dual-Tone Multi-Frequency keypad tones from
WAV PCM audio into timestamped segments so QA-tier callers can exercise the
`document/media` extraction path without a speech model, a network call, or
a new module dependency.

## Ownership boundary

This package owns the engine implementation only: WAV PCM parsing, Goertzel
tone detection, execution caps, and engine-level redaction. It does not wire
callers, add runtime flags, emit telemetry, persist facts, or enable hosted
extraction. Extraction stays default-off; no production caller is claimed.

## Exported surface

- `Engine`, `Options` — bounded tone transcription behind `media.Engine`
- `EngineName`, `EngineVersion` — backend identity recorded in facts

See `doc.go` for the full godoc contract, including the backend decision.

## Dependencies

- `internal/collector/document/media` — the `Engine` contract only
- Go standard library otherwise; no network, filesystem, cgo, or model I/O

## Telemetry

None. Transcription runs inline in the calling request and inherits its
span. Runtime wiring must add attempt metrics, elapsed time, and resource
outcomes before any hosted enablement.

Collector Performance Evidence: baseline `go test
./internal/collector/document/media/dtmf -count=1` on go1.26.10 linux/arm64
passes 8 engine tests in 0.005s package time with backend local-dtmf 1.0.0.
Input shape is synthetic 8 kHz mono/stereo WAV fixtures of 1.0-3.2 s plus
cap-rejection cases that fail before full decode. Terminal counts are at
most 200 segments of at most 64 chars each with one 25 ms frame buffer.
Transcribe is single-pass O(n) PCM analysis with input (16 MiB default),
duration (120 s default), segment, and text caps, honors context
cancellation, and adds no worker, queue, graph, or storage work. It is safe
because every bound is checked before or during the single decode pass and
breaches fail loudly instead of truncating facts.

No-Observability-Change: the engine emits no metric, span, log, or status
row. The `Media transcript engine` row in
docs/public/observability/telemetry-coverage.md names
eshu_dp_facts_emitted_total and eshu_dp_facts_committed_total as the
covering signals once a caller is wired.

## Gotchas / invariants

- Only genuine tones become text. Speech-like or toneless audio yields zero
  segments; the engine never invents lexical content.
- Multi-digit runs (two or more consecutive digits) are withheld as
  potentially sensitive; only isolated single digits persist.
- The same key pressed twice splits into two events only after three silent
  frames (about 75 ms at 8 kHz); shorter pauses merge into one press.
- Segment IDs are opaque ordinals. Speaker labels and mention hints are
  never emitted.
- Cap breaches fail loudly. The engine never truncates facts silently.
- Output is bit-identical for identical input and options.

## Related docs

- `go/internal/collector/document/media/README.md`
- Design rationale: `docs/internal/design/1737-visual-media-documentation-ingestion.md`
