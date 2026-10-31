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
