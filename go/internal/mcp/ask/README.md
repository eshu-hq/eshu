# MCP Ask registration and route selection

## Purpose

This package owns the MCP tool definition and pure internal-request selection
for Ask Eshu's natural-language answer surface.

## Ownership boundary

This package owns registration data, Ask family membership, and the pure
mapping from decoded arguments to a dependency-neutral internal request.
`internal/mcp` retains the tool's global position, global route fanout, the
private adapter, HTTP dispatch, authorization, response envelopes, summaries,
and transport telemetry. `internal/query` owns the Ask handler, default-off
checks, provider use, answer construction, and truth metadata.

## Exported surface

- `Tools` returns the single `ask` definition.
- `Route` selects the Ask request without executing it.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/mcp/toolcontract` owns the dependency-neutral `ToolDefinition`
  shape returned by `Tools`.
- `internal/mcp/routecontract` owns the dependency-neutral decoded-argument and
  internal-request shapes used by `Route`.

## Telemetry

None. Registration and route selection only construct in-memory values. The
parent MCP package keeps transport and dispatch signals, while the Ask HTTP
handler retains
`eshu_dp_api_request_duration_seconds` and
`eshu_dp_api_request_errors_total`.

## Gotchas / invariants

- The import path ends in `ask`, while the declared package is `asktools`. The
  root uses an explicit import alias.
- `Tools` returns a fresh definition. Mutating one result must not change a
  later result.
- The root registry keeps `ask` at position 160 of 162, after
  `trace_exposure_path` and before `list_relationship_edges`.
- Registration advertises the default-off requirement. Runtime enforcement
  remains in the query handler and server wiring.
- `Route` must return `handled=false` for unrelated tools and must not execute
  HTTP requests, query data, or enforce the default-off gate.
- Keep global fanout, the root adapter, HTTP dispatch, query execution,
  provider calls, authorization, and telemetry out of this package.

No-Observability-Change: this extraction moves only pure Ask route selection.
The root adapter still feeds the same global fanout, dispatch, authorization,
summaries, and transport telemetry, and the same query handler executes the
request.

## Related docs

- [MCP package](../README.md)
- [MCP route contract](../routecontract/README.md)
- [MCP tool contract](../toolcontract/README.md)
- [Ask local proof](../../../../docs/public/reference/local-testing/ask-eshu-local-proof.md)

## Verification

From `go/`, run `go test ./internal/mcp/... -count=1` and
`go vet ./internal/mcp/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
