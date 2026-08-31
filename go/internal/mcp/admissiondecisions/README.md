# MCP admission-decisions route selection

## Purpose

This package owns family membership and pure internal-request selection for the
MCP admission-decisions tool: the bounded listing of the reducer's correlation
admission decisions, which explain why a candidate was admitted, rejected,
ambiguous, stale, missing evidence, or hidden by permissions before or beside a
canonical graph edge.

## Ownership boundary

This package owns admission-decisions family membership and the mapping from
decoded arguments to a dependency-neutral internal request. `internal/mcp` keeps
tool registration and its client-visible order, global route fanout, the private
adapter, HTTP dispatch, authorization, timeouts, response budgets, envelopes,
summaries, and telemetry. `internal/query` owns the bounded read this path
reaches, including the required-key check, the state vocabulary, the anchor-pair
rule, the 1-200 limit bound, and the per-decision evidence cap.

## Exported surface

- `Route` selects the internal request for an admission-decisions tool without
  executing it, and reports `handled=false` for every other tool.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/mcp/routecontract` owns the dependency-neutral decoded-argument and
  internal-request shapes used by `Route`.

## Telemetry

None. Route selection only constructs in-memory values. The parent MCP package
keeps transport and dispatch signals, while the HTTP handler retains the shared
API request duration and error metrics and its own `SpanQueryAdmissionDecisions`
span.

## Gotchas / invariants

- The import path ends in `admissiondecisions`, while the declared package is
  `admissiondecisionstools`. The root uses an explicit import alias.
- The query carries exactly eight keys: `anchor_id`, `anchor_kind`, `domain`,
  `generation_id`, `include_evidence`, `limit`, `scope_id`, and `state`. The
  handler reads each by name with no catch-all, and a dropped key fails three
  different ways. `domain`, `scope_id`, and `generation_id` are required, so
  losing any of them returns 400. `anchor_kind` and `anchor_id` must arrive
  together, so losing one half returns 400 while losing both silently widens
  the page past the anchor the caller named. `state`, `include_evidence`, and
  `limit` each have a handler default, so losing one returns 200 with a wider
  state set, no evidence rows, or a 50-row page the caller did not ask for.
- Every key is sent even when the caller omitted it, so the handler sees an
  explicitly empty filter rather than no filter key at all. `include_evidence`
  in particular is always `"true"` or `"false"`, never absent.
- `include_evidence` honours only a Go bool. `routecontract.Arguments.BoolOr`
  does not parse strings, so `"true"` and `"1"` fall back to `false` and the
  caller gets a 200 with no evidence rows rather than an error. The advertised
  schema types the field as boolean, so this only bites clients that
  stringify their arguments.
- `limit` defaults to 50. That is the dispatcher's historical default, and it
  happens to match the handler's own default, so the handler cannot tell a
  caller who omitted `limit` from one who asked for 50. The handler still
  enforces its 1-200 bound; a zero, negative, or over-200 value is forwarded
  as-is and corrected there, not here.
- Numeric coercion follows `routecontract.Arguments.IntOr`: `int`, `int64`, and
  `float64` are accepted, a `float64` truncates toward zero, and every other
  type falls back to the default.
- The listing has no cursor, no `offset`, no `group_by`, and no aggregate
  sibling; it is limit-bounded only, and the handler reports `truncated` when
  more rows exist.
- `Route` returns a fresh query map per call, so a caller may mutate one result
  without changing a later one or the arguments it was given.

No-Observability-Change: this extraction moves only pure admission-decisions
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
