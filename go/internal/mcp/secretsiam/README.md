# MCP secrets/IAM route selection

## Purpose

This package owns family membership and pure internal-request selection for the
five MCP secrets/IAM tools: the identity trust chain, privilege posture
observation, secret access path, and posture gap listings, plus the
scope-anchored posture summary.

## Ownership boundary

This package owns secrets/IAM family membership and the pure mapping from
decoded arguments to a dependency-neutral internal request. `internal/mcp` keeps
tool registration and its client-visible order, global route fanout, the private
adapter, HTTP dispatch, authorization, timeouts, response budgets, envelopes,
summaries, and telemetry. `internal/query` owns the bounded reads these paths
reach, including the required scope anchor, the 1-200 limit bound, and the
keyset paging each listing pages with.

## Exported surface

- `Route` selects the internal request for a secrets/IAM tool without executing
  it, and reports `handled=false` for every other tool.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/mcp/routecontract` owns the dependency-neutral decoded-argument and
  internal-request shapes used by `Route`.

## Telemetry

None. Route selection only constructs in-memory values. The parent MCP package
keeps transport and dispatch signals, while the HTTP handlers retain the shared
API request duration and error metrics.

## Gotchas / invariants

- The import path ends in `secretsiam`, while the declared package is
  `secretsiamtools`. The root uses an explicit import alias.
- The summary is not a listing. `count_secrets_iam_posture` carries `scope_id`
  and nothing else — no `limit`, no cursor, no filter. It aggregates a whole
  scope, so there is no page to size and nothing to seek past. Forwarding a
  limit would not bound anything either: the handler never reads one, so the key
  would be inert and would advertise a bound the endpoint does not honor.
  Adding a key here looks like a consistency fix and is not one.
- `limit` defaults to 50 on each of the four listings. That is the dispatcher's
  historical default, not a handler default, and the handlers still enforce
  their own 1-200 bound.
- `scope_id` is always sent, on every route, even when the caller omitted it, so
  the handler sees an explicitly empty anchor and rejects it rather than reading
  an unscoped aggregate.
- Numeric coercion follows `routecontract.Arguments.IntOr`: `int`, `int64`, and
  `float64` are accepted, a `float64` truncates toward zero, and every other
  type — `float32` and a numeric string included — falls back to the default.
- Each listing carries its own cursor key (`after_chain_id`,
  `after_observation_id`, `after_path_id`, `after_gap_id`). They are not
  interchangeable: paging is keyset-only, with no `offset` and no `group_by`
  anywhere in the family.
- `Route` returns a fresh query map per call, so a caller may mutate one result
  without changing a later one or the arguments it was given.

No-Observability-Change: this extraction moves only pure secrets/IAM route
selection. The root adapter still feeds the same global fanout, dispatch,
authorization, budgets, envelopes, summaries, and transport telemetry, and the
same query handlers execute the requests.

## Related docs

- [MCP package](../README.md)
- [MCP route contract](../routecontract/README.md)
- [HTTP API reference](../../../../docs/public/reference/http-api.md)

## Verification

From `go/`, run `go test ./internal/mcp/... -count=1` and
`go vet ./internal/mcp/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
