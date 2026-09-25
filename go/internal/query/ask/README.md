# ask

## Purpose

Serves `POST /api/v0/ask`: the natural-language answer endpoint, with an
SSE stream variant at the same route under `Accept: text/event-stream`. See
`doc.go` for the contract.

## Ownership boundary

Owns the handler, the answer packet assembly (`answer_packet.go`,
`answer_packet_metadata.go`), the substance/runtime guardrails
(`guardrails.go`), and the SSE serving path (`sse.go`). The engine
(`Asker`) is injected — built by `internal/askwiring` for cmd/api and
cmd/mcp-server. The answer/evidence vocabulary lives in
`internal/query/querycontract`; auth gates live in `internal/query/auth`.

## Layout

- `handler.go` — `Handler`, auth gating, the ask/SSE dispatch, response
  shaping.
- `guardrails.go` — substance, runtime, prose-fallback, and publish-safety
  guardrails.
- `sse.go` — the SSE stream path with validated token deltas.
- `answer_packet.go` — `AnswerPacket` assembly, truth classification.
- `answer_packet_metadata.go` — metadata-driven packet composition plus the
  `Attach*` entry points wired into the impact shim.
- `capability.go` — `Capability` and `Support`, the single declaration of
  the row.

## Dependencies

`internal/query/querycontract` (capability gate, response writers, row
helpers, truth envelope), `internal/query/querycontract/answer`,
`internal/query/querycontract/evidence`, `internal/query/auth` (permission
gates), `internal/answerguardrail`, `internal/ask/{facet,render}`,
`internal/query/metrics` (tests only, the #3381 middleware regression).
The root query package imports this package for aliases only; this package
must never import root.

## Telemetry

Unchanged by the #6642 move: engine failures log `ask engine error`
(`err_type=engine_failure`, with SSE and SSE-sync variants) via the
injected logger, defaulting to `slog.Default()` when nil.
