# MCP content route selection

## Purpose

This package owns family membership and pure internal-request selection for
the five MCP content tools: repo-relative file read, repo-relative
line-range read, bounded evidence-citation-packet hydration, indexed
file-content search, and cached entity-source-snippet search.

## Ownership boundary

This package owns content family membership and the mapping from decoded
arguments to a dependency-neutral internal request. `internal/mcp` keeps
tool registration and its order (all five definitions stay together in
`tools_content.go`, alongside `get_entity_content`, whose own route
selection lives in `entityresolution` instead), global route fanout, the
private `contentRoute` adapter in `dispatch.go`, HTTP dispatch,
authorization, timeouts, response budgets, envelopes, summaries, and
telemetry. `internal/query` owns the bounded reads behind
`POST /api/v0/content/files/read`, `POST /api/v0/content/files/lines`,
`POST /api/v0/evidence/citations`, `POST /api/v0/content/files/search`, and
`POST /api/v0/content/entities/search`, including every limit default and
clamp described below.

## Exported surface

- `Route` selects the internal request for a content tool without executing
  it, and reports `handled=false` for every other tool.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/mcp/routecontract` owns the dependency-neutral decoded-argument
  and internal-request shapes used by `Route`.

## Telemetry

None. Route selection only constructs in-memory values. The parent MCP
package keeps transport and dispatch signals, while the HTTP handlers retain
the shared API request duration and error metrics (`request_metrics.go` in
`internal/query`).

## Gotchas / invariants

- The import path ends in `content`, while the declared package is
  `contenttools`. The root imports it with an explicit alias, matching the
  `deadcodetools`/`codeinteltools` convention.
- `get_file_lines` is the one body in this family that is NOT freshly
  built: it forwards the caller's decoded arguments verbatim (the same
  aliasing the root switch arm produced), so the handler alone validates
  `start_line`, `end_line`, `repo_id`, and `relative_path`. Every other
  route in this package builds a fresh map.
- `build_evidence_citation_packet` forwards `subject` and `handles`
  unchanged (`nil` when absent) and defaults `limit` to 10. The handler
  (`internal/query/evidence_citation.go`) independently defaults an
  absent/nonpositive limit to 10 and caps anything above 50
  (`evidenceCitationMaxLimit`) at 50; the advertised schema caps the
  incoming `handles` array at 500 (`evidenceCitationMaxInputHandles`).
- `search_file_content` and `search_entity_content` share
  `contentSearchBody`: `query` prefers the `query` argument and falls back
  to `pattern`; the repo scope collapses to a single `repo_id` when zero or
  one selector is supplied and switches to `repo_ids` only when more than
  one is supplied, because the handler (`internal/query/content_handler.go`)
  accepts only one of the two shapes per call. `limit` defaults to 10 here
  — this selector's own choice, matching the advertised schema default —
  which differs from the handler's own default of 50
  (`contentSearchDefaultLimit`); the handler clamps anything above 200
  (`contentSearchMaxLimit`) down to 200 rather than rejecting it.
- Numeric coercion follows `routecontract.Arguments.IntOr`: `int`, `int64`,
  and `float64` are honoured, a `float64` truncates toward zero, and every
  other type — including a stringified `"10"` — falls back to the default.
- Family membership is an explicit name switch, never a prefix match:
  `get_entity_content` shares the `content` stem but belongs to
  `entityresolution`, which shares no helper with this package.

No-Observability-Change: this extraction moves only pure content route
selection. The root adapter still feeds the same global fanout, dispatch,
authorization, budgets, envelopes, summaries, and transport telemetry, and
the same query handlers execute the requests.

## Related docs

- [MCP package](../README.md)
- [MCP route contract](../routecontract/README.md)
- [HTTP API reference](../../../../docs/public/reference/http-api.md)

## Verification

From `go/`, run `go test ./internal/mcp/... -count=1` and
`go vet ./internal/mcp/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
