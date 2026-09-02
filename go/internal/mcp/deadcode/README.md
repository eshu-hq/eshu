# MCP dead-code route selection

## Purpose

This package owns family membership and pure internal-request selection for
the three MCP dead-code tools: the bounded single-repository scan, the
paginated per-language investigation, and the cross-repository check that
asks whether named consumer repositories still call a producer repository's
exports.

## Ownership boundary

This package owns dead-code family membership and the mapping from decoded
arguments to a dependency-neutral internal request. `internal/mcp` keeps tool
registration order (`find_dead_code` lives in the root codebase group in
`tools_codebase.go`, `investigate_dead_code` in `tools_dead_code.go`, and
`find_cross_repo_dead_code` in `tools_cross_repo_dead_code.go`), global route
fanout, the private `deadCodeRoute` adapter in `dispatch.go`, HTTP dispatch,
authorization, timeouts, response budgets, envelopes, summaries, and
telemetry. `internal/query` owns the bounded reads behind the three
`/api/v0/code/dead-code` paths, including the limit clamp and the
investigation offset cap.

## Exported surface

- `Route` selects the internal request for a dead-code tool without executing
  it, and reports `handled=false` for every other tool.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/mcp/routecontract` owns the dependency-neutral decoded-argument
  and internal-request shapes used by `Route`.

## Telemetry

None. Route selection only constructs in-memory values. The parent MCP package
keeps transport and dispatch signals, while the HTTP handlers retain the shared
API request duration and error metrics (`request_metrics.go` in
`internal/query`) and their own scan-limit analysis metadata.

## Gotchas / invariants

- The import path ends in `deadcode`, while the declared package is
  `deadcodetools`. The root imports it with an explicit alias.
- Every request is a `POST` under `/api/v0/code/dead-code` with a JSON body
  and no query string, built fresh per call.
- Only the cross-repo handler rejects a blank `repo_id` (`repo_id is
  required`). The scan and investigate handlers accept one and widen to every
  repository the caller's scope grants, which is why `repo_id` still travels
  as an explicit empty string rather than being dropped.
- `limit` defaults to 100 here, the same value the handlers substitute for
  any limit at or below zero before clamping anything above 500 down to 500
  (`deadCodeDefaultLimit` and `deadCodeMaxLimit` in query's
  `code_dead_code.go`), so the dispatcher's default is indistinguishable from
  an omitted limit at the handler and no limit value can 400. `offset`
  defaults to 0; the investigate handler floors negatives and caps it at
  2000.
- `exclude_decorated_with` travels as a nil `[]any` (JSON `null`) when absent
  or malformed and as a non-nil empty `[]any` (JSON `[]`) when the caller
  sent an empty list. `consumer_repo_ids` is always a non-nil `[]string`
  whose empty-string and non-string members are dropped, so an absent value
  serializes as `[]`. The handlers decode both into the same empty
  `[]string`, but the bytes on the wire are inherited contract, so the tests
  pin nil-ness rather than length.
- Numeric coercion follows `routecontract.Arguments.IntOr`: `int`, `int64`,
  and `float64` are honoured, a `float64` truncates toward zero, and every
  other type — including a stringified `"25"` — falls back to the default.
- Family membership is an explicit name switch, never a prefix match:
  `find_dead_iac` shares the `find_dead_` spelling but belongs to the IaC
  family that stays in the root switch.

No-Observability-Change: this extraction moves only pure dead-code route
selection. The root adapter still feeds the same global fanout, dispatch,
authorization, budgets, envelopes, summaries, and transport telemetry, and the
same query handlers execute the requests.

## Related docs

- [MCP package](../README.md)
- [MCP route contract](../routecontract/README.md)
- [HTTP API reference](../../../../docs/public/reference/http-api.md)
- [Dead-code reachability spec](../../../../docs/public/reference/dead-code-reachability-spec.md)

## Verification

From `go/`, run `go test ./internal/mcp/... -count=1` and
`go vet ./internal/mcp/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
