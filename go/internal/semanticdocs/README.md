# Semantic Docs

## Purpose

`semanticdocs` converts bounded documentation sections and mocked semantic
observation output into `semantic.documentation_observation` fact envelopes. It
proves the fact-construction contract for #1758 without enabling provider
calls, queues, reducers, query routes, or graph writes.

## Ownership boundary

This package owns pure envelope construction for semantic documentation
observations. It accepts `doctruth.SectionInput` values that already came from
documentation collectors or deterministic documentation truth extraction, then
validates the semantic payload against `facts`.

It does not call LLM providers, store credentials, persist rows, run workers,
write NornicDB, expose HTTP/MCP readbacks, or promote model output into
service, deployment, runtime, vulnerability, infrastructure, or ownership
truth.

## Exported surface

See `doc.go` for the godoc-rendered contract.

- `ProviderProfile` is the display-safe provider-profile handle. It carries no
  secret values or request bodies.
- `MockObservation` is fixture-facing parsed observation output.
- `Config` supplies prompt, redaction, extractor, policy, freshness, and
  provider-profile provenance.
- `Emitter` validates configuration and builds fact envelopes.
- `NewEmitter` constructs an emitter after fail-closed validation.
- `Emitter.Emit` returns source-derived
  `semantic.documentation_observation` envelopes for one bounded section.

## Dependencies

The package imports only the standard library plus `internal/doctruth` and
`internal/facts`. It remains a leaf for provider and storage behavior: callers
own any future queueing, persistence, provider execution, and readback.

## Telemetry

None directly. The package has no runtime side effects. Future worker or query
integrations must add operator-visible metrics, spans, logs, status fields, and
audit records before enabling hosted, local, Compose, or assistant-mediated
semantic extraction.

No-Observability-Change: this package is a pure, mock-only fact builder. It
does not add a worker, provider request, queue consumer, graph write, database
query, HTTP handler, MCP tool, or runtime stage.

## Gotchas / invariants

- Observations are evidence only. `Emitter` validates that admission stays
  `provenance_only` or `documentation_finding_candidate`; it never emits
  canonical truth.
- Provider identity is a profile handle. Raw provider keys, bearer tokens,
  request bodies, prompt payloads, and private provider responses do not belong
  in this package.
- Unsafe redaction state forces `provenance_only` and drops observation text.
  The caller must not try to rescue unsafe model text here.
- Stable fact identity is based on replay provenance such as source hash, chunk
  hash, prompt version, redaction version, extractor version, and provider
  profile. Display text can change without changing the replay identity unless
  the caller supplies a different observation hash.
- `doctruth` remains the deterministic extraction package. Do not move broad
  prose inference or provider calls into `doctruth`.

## Related docs

- `docs/internal/design/1758-documentation-semantic-observations.md`
- `docs/public/reference/fact-envelope-reference.md`
- `go/internal/doctruth/README.md`
- `go/internal/facts/README.md`
