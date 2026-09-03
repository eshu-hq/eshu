# MCP entity-resolution route selection

## Purpose

This package owns family membership and pure internal-request selection for
the three MCP entity-resolution tools: the exact case-sensitive entity name
lookup, the canonical entity context read, and the exact entity content read.

## Ownership boundary

This package owns entity-resolution family membership and the mapping from
decoded arguments to a dependency-neutral internal request. `internal/mcp`
keeps tool registration order (`resolve_entity` and `get_entity_context` live
in the root context group in `tools_context.go`, and `get_entity_content` in
`tools_content.go`), global route fanout, the private `entityResolutionRoute`
adapter in `dispatch.go`, HTTP dispatch, authorization, timeouts, response
budgets, envelopes, summaries, and telemetry. `internal/query` owns the
bounded reads behind `/api/v0/entities/resolve`,
`/api/v0/entities/{entity_id}/context`, and `/api/v0/content/entities/read`,
including the resolve limit clamp and every required-field rejection.

## Exported surface

- `Route` selects the internal request for an entity-resolution tool without
  executing it, and reports `handled=false` for every other tool.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/mcp/routecontract` owns the dependency-neutral decoded-argument
  and internal-request shapes used by `Route`.

## Telemetry

None. Route selection only constructs in-memory values. The parent MCP package
keeps transport and dispatch signals, while the HTTP handlers retain the shared
API request duration and error metrics (`request_metrics.go` in
`internal/query`); the entity-context read additionally keeps its own
degraded-read metrics on `EntityHandler.Instruments`.

## Gotchas / invariants

- The import path ends in `entityresolution`, while the declared package is
  `entityresolutiontools`. The root imports it with an explicit alias.
- `resolve_entity` and `get_entity_content` are `POST` with a JSON body and a
  nil query; `get_entity_context` is the family's one `GET`, with a nil body
  and an always-non-nil query map that is empty when `environment` is blank —
  the exact shape the root arm built.
- `resolve_entity`'s `name` maps from the advertised `query` argument only
  when the deprecated `name` alias is blank, and stays absent when both are
  blank; the handler then rejects with HTTP 400 ("name is required") rather
  than this builder inventing one.
- `resolve_entity`'s `type` prefers the single `type` argument and falls back
  to the first element of the deprecated `types` array — a non-empty array
  whose first element is not a string still sets `type`, to an explicit empty
  string, exactly as the root helper behaved. `repo_id` travels only when
  non-empty.
- `resolve_entity`'s `limit` defaults to 10, the same value the handler
  substitutes for any limit at or below zero before capping anything above
  100 at 100 (`normalizeResolveEntityLimit` in query's
  `entity_resolve_page.go`), so the dispatcher's default is indistinguishable
  from an omitted limit at the handler and no limit value can 400. The
  handler's other bounds are rejections, not clamps: a global call without
  `repo_id` requires a supported `type` or a canonical content-entity handle,
  graph-only types require `repo_id`, and an unknown non-blank type rejects
  with HTTP 400.
- `get_entity_context` path-escapes the entity id into
  `/api/v0/entities/{entity_id}/context` and forwards `environment` only when
  non-empty. The handler (`getEntityContext` in query's `entity.go`) reads
  only the `entity_id` path parameter and decodes no query parameter at all,
  so `environment` is an inherited advertised-versus-decoded asymmetry, not a
  dropped field.
- `get_entity_content` always sends `entity_id`, as an explicit empty string
  when absent or wrong-typed; the handler (`readEntity` in query's
  `content_handler.go`) decodes only `entity_id` and rejects the empty string
  with HTTP 400 ("entity_id is required").
- Numeric coercion follows `routecontract.Arguments.IntOr`: `int`, `int64`,
  and `float64` are honoured, a `float64` truncates toward zero, and every
  other type — including a stringified `"17"` — falls back to the default.
- Family membership is an explicit name switch, never a prefix match:
    `search_entity_content` shares the entity spelling but is not part of this
    family. Its whole body comes from `contentSearchBody`, the builder it
    shares with `search_file_content`, and that pair moved together into the
    `content` child, which now owns both the helper and the routes.

No-Observability-Change: this extraction moves only pure entity-resolution
route selection. The root adapter still feeds the same global fanout,
dispatch, authorization, budgets, envelopes, summaries, and transport
telemetry, and the same query handlers execute the requests.

## Related docs

- [MCP package](../README.md)
- [MCP route contract](../routecontract/README.md)
- [HTTP API reference](../../../../docs/public/reference/http-api.md)

## Verification

From `go/`, run `go test ./internal/mcp/... -count=1` and
`go vet ./internal/mcp/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
