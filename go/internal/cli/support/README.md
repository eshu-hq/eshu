# CLI Support HTTP

This package serves the CLI-support endpoints for the extracted thin CLI
(8874 S4): packet rendering, offline report composition, bundle capture and
validation, and answer-quality scoring.

## Ownership

Each route wraps one local-only core function the CLI cannot carry after
extraction. The handlers are pure functions of the request body: no graph,
Postgres, collector, or queue reads. The package has zero construction
dependencies and mounts additively from `go/cmd/api` wiring, beside
`serviceintelhttp` — never from the `query` router, or the build cycles
(`support` imports `query`, `serviceintel`, `reportbundle`, and
`answerquality`).

## Dependencies

- `query` — envelope writers (`WriteSuccess`, `WriteErrorEnvelope`), packet
  rendering, truth/profile/freshness contracts.
- `serviceintel` — report composition over supplied sections.
- `reportbundle` — bundle capture and validation.
- `answerquality` — evidence scoring.

## Telemetry

None per-handler. `query.RequestMetricsMiddleware` records per-endpoint
duration/error metrics for every `apiMux` route, and auth flows through the
existing `wrapAPIAuth` layer. New endpoints inherit both by mounting on the
application mux.

## Gotchas / invariants

- Bounded bodies (`MaxBytesReader` per endpoint); oversize fails closed
  with 413 before any decode work.
- The render route returns raw bytes with the format content type, not an
  envelope: the caller asked for a rendering, not a query answer.
- Capture errors map to 422 (refusal class — every failure reachable over
  the wire is an input refusal, since the envelope already survived a JSON
  round trip client-side). The secret is never echoed.
- Validate always answers 200 with `{valid, message}` — the verdict is the
  product, and the CLI maps it to its exit code.
- Keep the route, its OpenAPI fragment
  (`go/internal/query/openapi/paths/cli/`), and
  `docs/public/reference/http-api/cli-support-endpoints.md` in lockstep.
  No MCP tools: these are CLI-support endpoints, not query capabilities.
