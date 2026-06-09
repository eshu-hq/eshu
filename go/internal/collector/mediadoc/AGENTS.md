# AGENTS.md - collector/mediadoc guidance for LLM assistants

## Read first

1. `go/internal/collector/mediadoc/README.md`
2. `go/internal/collector/mediadoc/doc.go`
3. `go/internal/collector/mediadoc/extract.go`
4. `go/internal/collector/mediapreflight/README.md`
5. `docs/internal/design/1737-visual-media-documentation-ingestion.md`

## Invariants

- Keep transcript extraction default-off. Do not wire this package into hosted
  collectors, repository discovery, CLI flags, Helm values, or Compose profiles
  without a separate security-reviewed issue.
- Emit source facts only: `documentation_document` and
  `documentation_section`. Do not emit entity mentions, claim candidates, graph
  edges, service truth, deployment truth, incident truth, or ownership truth
  from transcript text.
- Run `mediapreflight` before calling a transcript engine. Non-WAV codecs stay
  unsupported until a codec or container dependency review lands.
- Do not persist raw media bytes, audio samples, video frames, subtitle text,
  speaker names, local paths, private URLs, credentials, usernames, attendee
  lists, or sensitive transcript text.
- Warning classes must stay low-cardinality and design-owned.

## Common changes and how to scope them

- Add transcript engine behavior by extending the injected `Engine` contract and
  testing it with synthetic fixtures.
- Add redaction classes by updating extractor tests, the README, and the
  redaction helper. Keep raw matching values out of persisted content.
- Add runtime telemetry only in the caller that enables extraction. This
  package has no metric, span, log, or status side effects.

## Failure modes

- Unsupported codecs, malformed media, resource limits, and canceled preflight
  emit a skipped document fact with warning metadata and no section facts.
- Safe media with no transcript segments emits a document fact with
  `transcript_status=no_text`.
- Media preflight no-speech warnings emit a document fact with
  `transcript_status=no_speech`.
- Sensitive transcript segments persist redacted content with hashes and
  redaction metadata.

## Anti-patterns

- Adding cloud transcription, model runtimes, network fetches, codec execution,
  subtitle trust, or temp file handling here.
- Treating transcript text as canonical service, deployment, owner, incident, or
  graph truth.
- Hiding malformed or unsupported media as an empty successful document.
- Adding route, MCP, graph, reducer, or storage logic to this package.

## What not to change without review

- Runtime enablement, sandbox settings, transcription dependencies, codec
  decoding, subtitle ingestion, raw excerpt return policy, or any graph/reducer
  materialization path require a separate reviewed design update.
