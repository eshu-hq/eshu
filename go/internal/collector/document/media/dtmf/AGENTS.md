# AGENTS.md - media/dtmf guidance for LLM assistants

## Read first

1. `go/internal/collector/document/media/dtmf/README.md`
2. `go/internal/collector/document/media/dtmf/doc.go`
3. `go/internal/collector/document/media/dtmf/engine.go`
4. `go/internal/collector/document/media/AGENTS.md`

## Invariants

- Keep transcription local, deterministic, and dependency-free. Do not add
  model runtimes, network fetches, cloud APIs, temp files, cgo, or codec
  execution beyond bounded WAV PCM parsing.
- Never invent lexical content. Only tones genuinely present in the signal
  become segment text.
- Withhold multi-digit runs. Keep segment IDs opaque, speaker labels empty,
  and mention hints absent.
- Fail loudly on cap breaches. Never truncate or drop content silently
  without a documented rule.
- Keep extraction default-off. No caller wiring, runtime flags, telemetry,
  or hosted paths in this package.

## Common changes and how to scope them

- Tune detection thresholds only with synthetic fixtures and before/after
  measurements on tone, noise, and boundary cases.
- Add redaction classes by updating engine tests, the README, and the
  withholding rule together.

## Failure modes

- Oversized input, over-duration audio, segment overflow, malformed WAV,
  unsupported format, and cancelled context return errors and no segments.
- Toneless audio returns zero segments; downstream extraction records
  `transcript_status=no_text`.
- Withheld digit runs return no segment; they never reach facts.

## Anti-patterns

- Speech-to-text claims without an acoustic model.
- Persisting raw digits, speaker identity, or source locations.
- Adding caller, gate, telemetry, or deployment logic here.

## What not to change without review

- Detection semantics, redaction policy, cap defaults, or any new
  dependency require a reviewed design update.
