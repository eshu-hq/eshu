# answernarration

## Purpose

`answernarration` validates optional narrated answer text against an existing
Eshu answer packet, explicit citation allowlist, and caller-supplied bounds. It
gives future governed narration work a pure, testable contract before any
provider or assistant exchange exists.

## Ownership boundary

This package owns sentence-level narration validation only. It does not build
prompts, call providers, hydrate citations, read graph or content stores,
modify answer packets, change truth labels, or decide whether a hosted policy
allows narration work.

## Exported surface

See `doc.go` for the godoc contract. The package exports `Validate`, the
candidate `Narration` and `Sentence` shapes, provenance enums, and
low-cardinality `Reason` values suitable for future status, audit, and metrics
surfaces.

## Dependencies

The package imports `internal/query` for `AnswerPacket` and `AnswerTruthClass`.
It uses only the Go standard library otherwise.

## Telemetry

No-Observability-Change: validation is a pure function. It emits no metrics,
spans, logs, status rows, audit events, network calls, graph queries, content
reads, or provider requests.

## Gotchas / invariants

- The source `ResponseEnvelope` and `AnswerPacket` stay canonical.
- A factual narrated sentence must cite packet-owned provenance.
- Narration must not hide unsupported, partial, truncated, stale, or
  missing-evidence state.
- Narration must not promote derived, fallback, code-hint, semantic-observation,
  or unsupported truth into authoritative graph truth.
- Publish-safety checks are conservative and reject credential-like material,
  private paths, private hostnames, raw prompt text, and provider-response text.

## Related docs

- `docs/internal/design/2462-governed-answer-narration.md`
- `docs/public/reference/answer-packets.md`
- `docs/public/reference/reading-answers.md`
- `docs/public/reference/truth-label-protocol.md`
