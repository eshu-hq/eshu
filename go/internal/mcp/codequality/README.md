# MCP complexity/quality route selection

## Purpose

This package owns family membership and pure internal-request selection for
the three MCP complexity/quality tools: the single-function cyclomatic
complexity lookup, the most-complex-functions ranking, and the bounded
code-quality inspection over complexity, function length, argument count, and
refactoring candidates.

## Ownership boundary

This package owns complexity/quality family membership and the mapping from
decoded arguments to a dependency-neutral internal request. `internal/mcp`
keeps tool registration order (`calculate_cyclomatic_complexity` and
`find_most_complex_functions` live in the root codebase group in
`tools_codebase.go`, and `inspect_code_quality` in `tools_code_quality.go`),
global route fanout, the private `codeQualityRoute` adapter in `dispatch.go`,
HTTP dispatch, authorization, timeouts, response budgets, envelopes,
summaries, and telemetry. `internal/query` owns the bounded reads behind
`/api/v0/code/complexity` and `/api/v0/code/quality/inspect`, including the
limit clamps and the inspection offset cap.

## Exported surface

- `Route` selects the internal request for a complexity/quality tool without
  executing it, and reports `handled=false` for every other tool.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/mcp/routecontract` owns the dependency-neutral decoded-argument
  and internal-request shapes used by `Route`.

## Telemetry

None. Route selection only constructs in-memory values. The parent MCP package
keeps transport and dispatch signals, while the HTTP handlers retain the shared
API request duration and error metrics (`request_metrics.go` in
`internal/query`) and their graph-read error selectors.

## Gotchas / invariants

- The import path ends in `codequality`, while the declared package is
  `codequalitytools`. The root imports it with an explicit alias.
- Every request is a `POST` with a JSON body and no query string, built fresh
  per call. Both complexity tools share `/api/v0/code/complexity`; the
  handler branches on the selectors, answering a single-function lookup when
  `entity_id` or `function_name` is non-empty and the most-complex list
  otherwise.
- `calculate_cyclomatic_complexity` carries `entity_id` only when the caller
  supplied a non-empty string; absent, empty, and wrong-typed values leave
  the key out entirely, and the key's absence is the pinned wire shape. It
  sends no `limit` key at all, so a blank-selector call reaches the handler's
  own list default of 10. Its advertised schema also names `path` and
  `scope`; neither is selected here and the handler decodes neither — an
  inherited advertised-versus-dispatched asymmetry, not a dropped field.
- `limit` defaults to 10 on `find_most_complex_functions` and
  `inspect_code_quality`, the same value both handlers substitute for any
  limit at or below zero before clamping anything above 100 down to 100
  (`normalizeComplexityListLimit` in query's `code_complexity_page.go` and
  `normalizeCodeQualityLimit` in `code_quality.go`), so the dispatcher's
  default is indistinguishable from an omitted limit at the handler and no
  limit value can 400.
- `inspect_code_quality`'s `offset` bounds act in opposite directions: the
  handler floors a negative offset to 0 but rejects anything above 10000 with
  HTTP 400 ("offset exceeds maximum"). Do not describe both bounds as clamps.
- The `min_complexity`, `min_lines`, and `min_arguments` thresholds travel as
  0 when omitted so the handler resolves its own check-specific defaults
  (`min_lines` 20, `min_arguments` 5, `min_complexity` 1 for the complexity
  check and 10 otherwise). Forwarding a positive default here would pin one
  check's threshold onto every other check.
- A blank `check` resolves to `refactoring_candidates` at the handler; an
  unsupported non-blank check rejects with HTTP 400. A blank `repo_id` is
  accepted everywhere in this family and widens to every repository the
  caller's scope grants, which is why it still travels as an explicit empty
  string rather than being dropped.
- Numeric coercion follows `routecontract.Arguments.IntOr`: `int`, `int64`,
  and `float64` are honoured, a `float64` truncates toward zero, and every
  other type — including a stringified `"25"` — falls back to the default.
- Family membership is an explicit name switch, never a prefix match:
  `inspect_code_inventory` shares the `inspect_code_` spelling but belongs to
  the structural-inventory family that stays in the root switch.

No-Observability-Change: this extraction moves only pure complexity/quality
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
