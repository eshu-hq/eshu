# MCP CODEOWNERS ownership route selection

## Purpose

This package owns family membership and pure internal-request selection for the
MCP CODEOWNERS ownership tool: the bounded, keyset-paged listing of which owner
answers for which path in a repository.

## Ownership boundary

This package owns CODEOWNERS family membership and the mapping from decoded
arguments to a dependency-neutral internal request. `internal/mcp` keeps tool
registration and its client-visible order, global route fanout, the private
adapter, HTTP dispatch, authorization, timeouts, response budgets, envelopes,
summaries, and telemetry. `internal/query` owns the bounded read this path
reaches, including repository-access scoping, keyset paging, and the
`effective_owner` precedence between a service manifest and the CODEOWNERS file.

## Exported surface

- `Route` selects the internal request for a CODEOWNERS ownership tool without
  executing it, and reports `handled=false` for every other tool.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/mcp/routecontract` owns the dependency-neutral decoded-argument and
  internal-request shapes used by `Route`.

## Telemetry

None. Route selection only constructs in-memory values. The parent MCP package
keeps transport and dispatch signals, while the HTTP handler retains the shared
API request duration and error metrics.

## Gotchas / invariants

- The import path ends in `codeowners`, while the declared package is
  `codeownerstools`. The root uses an explicit import alias.
- `after_order_index` is the numeric leg of the three-part
  `after_order_index`/`after_pattern`/`after_ref` keyset cursor, and it is the
  one argument here that has no default. An absent key formats as the empty
  string, because the handler admits a cursor only when all three legs arrive:
  coercing an absent leg to `"0"` would turn a first page into a half-supplied
  cursor. A key the caller did send but typed wrong still formats as `"0"`,
  matching `routecontract.Arguments.IntOr`, because the caller did send the leg.
- `limit` defaults to 50. That is the dispatcher's historical default, not a
  handler default, and the handler still enforces its own bounds.
- Numeric coercion otherwise follows `routecontract.Arguments.IntOr`: `int`,
  `int64`, and `float64` are accepted, a `float64` truncates toward zero, and
  every other type falls back to the default.
- The query carries exactly five keys. The listing has no `offset`, no
  `group_by`, and no aggregate sibling; paging is keyset-only.
- `Route` returns a fresh query map per call, so a caller may mutate one result
  without changing a later one or the arguments it was given.

No-Observability-Change: this extraction moves only pure CODEOWNERS ownership
route selection. The root adapter still feeds the same global fanout, dispatch,
authorization, budgets, envelopes, summaries, and transport telemetry, and the
same query handler executes the request.

## Related docs

- [MCP package](../README.md)
- [MCP route contract](../routecontract/README.md)
- [HTTP API reference](../../../../docs/public/reference/http-api.md)

## Verification

From `go/`, run `go test ./internal/mcp/... -count=1` and
`go vet ./internal/mcp/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
