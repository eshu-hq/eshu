# MCP dead-code route selection

## Purpose

This package owns family membership and pure internal-request selection for
the three MCP dead-code tools: the bounded single-repository scan, the
paginated per-language investigation, and the cross-repository check that
asks whether named consumer repositories still call a producer repository's
exports.

## Ownership boundary

This package owns dead-code family membership, the mapping from decoded
arguments to a dependency-neutral internal request, and the tool
definitions. `internal/mcp` keeps the root registration wrapper and
client-visible order (the three definitions are spliced into the root
codebase group at their long-standing positions), global route fanout, the
private `deadCodeRoute` adapter in `dispatch.go`, HTTP dispatch,
authorization, timeouts, response budgets, envelopes, summaries, and
telemetry. `internal/query` owns the bounded reads behind the three
`/api/v0/code/dead-code` paths, including the limit clamp and the
investigation offset cap.

## Exported surface

- `Route` selects the internal request for a dead-code tool without executing
  it, and reports `handled=false` for every other tool.
- `Tools` returns the three dead-code tool definitions.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/mcp/contract/route` owns the dependency-neutral decoded-argument
  and internal-request shapes used by `Route`.

## Telemetry

None. Route selection only constructs in-memory values. The parent MCP package
keeps transport and dispatch signals, while the HTTP handlers retain the shared
API request duration and error metrics (`request.go` in
`internal/query/metrics`) and their own scan-limit analysis metadata.

## Gotchas / invariants

- The import path ends in `deadcode`, while the declared package is
  `deadcodetools`. The root imports it with an explicit alias.
- Every request is a `POST` under `/api/v0/code/dead-code` with a JSON body
  and no query string, built fresh per call.
- Only the cross-repo handler rejects a blank `repo_id` (`repo_id is
  required`). The scan and investigate handlers accept one and widen to every
  repository the caller's scope grants, which is why `repo_id` still travels
  as an explicit empty string rather than being dropped.
- `limit` defaults to 25 here (`defaultLimit`), and the schema advertises the
  same constant. The value is sized to the MCP response budget: 100 candidate
  rows exceeded it on most measured repositories (#7168). It is deliberately
  lower than the handlers' own default of 100, which they substitute for any
  limit at or below zero before clamping anything above 500 down to 500
  (`deadCodeDefaultLimit` and `deadCodeMaxLimit` in query's
  `code_dead_code.go`); an omitted MCP limit reaches the handler as 25, never
  as its 100, and no limit value can 400. `offset`
  defaults to 0, and unlike `limit` the investigate handler REJECTS rather than
  clamps it: `normalizeDeadCodeInvestigationRequest` returns an error for a
  negative offset and for one above `deadCodeInvestigationMaxOffset`, so either
  surfaces to the caller as HTTP 400. The two parameters are deliberately
  asymmetric — an out-of-range limit is silently corrected, an out-of-range
  offset is refused.
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

- [MCP package](../../README.md)
- [MCP route contract](../../contract/route/README.md)
- [HTTP API reference](../../../../../docs/public/reference/http-api.md)
- [Dead-code reachability spec](../../../../../docs/public/reference/dead-code-reachability-spec.md)

## Verification

From `go/`, run `go test ./internal/mcp/... -count=1` and
`go vet ./internal/mcp/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
