# Media Transcript Documentation

## Purpose

`collector/document/media` turns reviewed local transcript output for media
documentation artifacts into source-neutral documentation document and section
facts. It exists so audio/video transcript facts can be tested and read back
without enabling a hosted collector path or treating spoken text as operational
truth. Emitted metadata identifies transcript sections with the incident-media
`transcript_chunk` source class so later correlation code can preserve
provenance without promoting transcript text to graph truth.

## Ownership boundary

This package owns the post-preflight transcript fact boundary for local media
inputs. It calls `media` preflight first, invokes only an injected transcript
engine for supported inputs, redacts sensitive-looking transcript segments and
the segments that follow a sensitive keyword (see "Value segments"), and emits
`documentation_document`, timestamped `documentation_section`, and
provenance-only `documentation_entity_mention` envelopes.

It does not discover repositories, add runtime flags, create temp files, call
cloud transcription APIs, persist raw media bytes, read video frames, persist
speaker names, trust subtitles, emit graph edges, infer service or deployment
truth, or create claim candidates. Transcript mentions use the existing
`doctruth` extractor over redacted-safe section content; exact, ambiguous, and
unmatched mentions remain document evidence only. Runtime enablement still
requires sandbox, dependency, telemetry, ACL, and security review.

## Exported surface

See `doc.go` for the godoc-rendered package contract.

- `Engine` is the injected local transcript boundary.
- `Request` describes one media artifact revision and durable scope metadata.
- `Options` forwards media preflight limits and section text bounds.
- `Media`, `EngineResult`, and `Segment` describe bounded transcript input and
  output without source-specific types. `EngineResult.ModelName` and
  `ModelDigest` become `transcript_model` and `transcript_model_digest`
  section metadata when a model-backed engine sets them.
- `ErrNoSpeech` is the sentinel an engine wraps when it ran cleanly but heard
  no speech. `Extract` records it as `transcript_status=no_speech` with the
  `transcript_no_speech` warning instead of failing.
- `Extract` runs preflight, calls the engine when allowed, and returns
  ready-to-persist fact envelopes. Callers may pass known `doctruth.Entity`
  values and per-segment mention hints; this package emits mention facts only
  from non-redacted transcript sections and never passes claim hints.

## Dependencies

The package depends on `internal/collector/preflight/media` for metadata-only
media gating, `internal/doctruth` for conservative mention extraction,
`internal/facts` for documentation payload and envelope contracts, and
`internal/scope` for the documentation collector kind. It uses only the Go
standard library otherwise.

## Telemetry

None directly. This is a default-off helper with no runtime side effects. A
future runtime integration must add bounded extraction attempt metrics, warning
class counts, elapsed time, source bytes, duration, codec/container outcomes,
segment counts, redaction counts, logs, spans, and status evidence before
enablement.

Collector Performance Evidence: `go test ./internal/collector/document/media
./internal/collector/preflight/media ./internal/doctruth -count=1` proves
transcript fact construction is bounded by media preflight limits, synthetic
transcript segments, section text bounds, and deterministic mention extraction
without adding discovery, queue, graph, or runtime work.

No-Regression Evidence: the value-segment walk (#9786) adds a small cost. It
sorts the segments, capped at 1,000 by the `whispercpp` engine, and makes two
passes over them. Baseline is `d2e483870d`, measured at `9e2cb253b2`. A
throwaway benchmark of `buildSections` on 1,000 benign synthetic segments, 5
runs of 3,000 iterations each on arm64, went from a median of 8.27 ms/op to
8.43 ms/op, about 2% more, with about 1,004 more allocations per call (24,913
to 25,917). There is no backend, queue, or row count: this is in-memory work
with no production caller, and it holds no lock and starts no worker.

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

No-Observability-Change: the value-segment rule (#9786) adds none of these and
no error text. A value section carries the existing `redacted` and
`redaction_class` metadata, which is what a future caller counts.

## Value segments

A speech engine can put a secret's value in a later segment than the word that
introduces it, so `Extract` does not judge segments only one at a time (#9786).

- A segment with a sensitive keyword (`password`, `api_key`, `private_key`, or
  a `*_marker` keyword) opens a window of `valueWindowSegments` (2) segments.
  Those segments persist as `[redacted]`, with the same `redacted`,
  `redaction_class`, and warning metadata as a marker segment, and emit no
  mention facts.
- Emails, URLs, tickets, and local paths with no keyword in their text redact
  their own segment and open no window. They are values, not words that
  introduce one. A segment that has both a keyword and one of these patterns,
  such as "password alice@example.com", still opens a window, because the
  keyword alone decides that.
- The walk runs twice, and every keyword segment opens a window in both: once
  in time order, by start then end, because the `Engine` contract does not
  promise sorted output, and once in slice order, because timestamps cannot be
  trusted to place a keyword. A wrong time can still look valid and sort away
  from the value, and a duration bound does not help: the real engine returned
  an end of 6500 ms on the 5783 ms split fixture. The passes agree on
  time-sorted input. On out-of-order input the second pass only adds redaction,
  and that over-redaction is accepted policy. Section order and fallback
  section IDs stay in engine order.
- Only a segment with a letter or digit uses up the window. Blank,
  punctuation-only, and zero-width segments stay redacted inside it but leave
  it open, so "The password is", ".", "...", "swordfish." still covers the
  value. A long silence does not close it either: the window counts segments,
  not milliseconds.
- A segment with invalid timing never reaches a section and uses up no window,
  but a keyword in it still opens one: its text was spoken.
- Each keyword segment adds a full window, so stacked markers each cover their
  own value.
- The window is 2 so it covers an immediate value, plus a value the engine
  broke in two or a filler spoken before it. It stays small because every
  segment in the window loses its text.
- Any keyword opens the window, not only one that ends its segment. This is a
  deliberate deviation from a "marker-ending" rule. Through the real engine
  path (16 kHz stdin), `base.en` hears the fixture's "The password is as
  follows." as "The password in a column". Direct `whisper-cli` runs on the
  22.05 kHz file heard "as follows" as "El Falon", "Alpha alone", and "El
  Paolo". A heuristic that reads the words after a keyword fails open.
- The cost is lost speech. One keyword segment can redact two following
  segments. On valid, time-sorted input, n consecutive keyword segments can
  redact n+1 segments after the last one; a keyword segment with invalid timing
  arms a window without spending a slot, so a run of those can cost up to 2n.
  So "The password was rotated." costs ordinary speech. That loss
  shows in section metadata; a persisted secret cannot be taken back.

Known limits:

- A value spoken before its keyword ("swordfish is the password") is not
  redacted, because the window only looks forward (#9792).
- A value spoken past the window keeps its text.
- Two self-redacting segments in a row, such as an email and a ticket, use up
  the window, so a value behind them keeps its text.
- Only the keywords above are matched, so spoken forms such as "API key" open
  no window and are not redacted (#9788).

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
  keeps redaction metadata instead. Its `text_hash` and `excerpt_hash` are
  hashes of the `[redacted]` placeholder, because an unsalted sha256 of a short
  spoken value would confirm a guess at it. Redacted sections do not emit
  mention facts, even when the caller supplied mention hints.
- Sensitive-looking mention hints are dropped before doctruth extraction; a
  hint must not be used to bypass transcript redaction.
- Transcript text and transcript mentions remain document evidence. This
  package must not emit claim candidates, graph relationships, ownership truth,
  deployment truth, incident truth, or service truth.
- A caller must provide a transcript engine; there is no default engine or
  hosted provider fallback. In-tree backends are the out-of-process
  whisper.cpp speech-to-text engine in `whispercpp/` (#9729) and the
  deterministic DTMF decoder in `dtmf/`, kept as a fixture backend. Both are
  for explicit QA-tier injection only.

## Related docs

- `docs/internal/design/1737-visual-media-documentation-ingestion.md`
- `docs/public/reference/incident-media-evidence.md`
- `go/internal/collector/preflight/media/README.md`
- `docs/public/reference/local-testing.md`
