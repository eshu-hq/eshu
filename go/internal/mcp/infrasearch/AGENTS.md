# AGENTS.md — MCP infrastructure-search route guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` for MCP routing, authorization, and transport rules.
3. `routes.go` and `routes_test.go` for the child request-selection contract.
4. `../dispatch_infra_search.go` and `../dispatch_infra_search_contract_test.go`
   for the root adapter and the production-boundary proof.
5. `../dispatch.go` for `resolveRoute`, where the adapter is the last
   delegation consulted before the main switch this family used to sit in.
6. `../ecosystem/infra_search_tools.go` for the advertised schema, which stays
   in the ecosystem child and must keep naming the same eight fields, the
   same six `category` values, and the same 1..200 `limit` range.
7. `../routecontract/README.md` for the dependency-neutral request contract.
8. `go/internal/query/infra.go` for the handler that decodes the keys this
   package selects: the scope rule, the category vocabulary, the capability
   check, and the asymmetric `limit` bound all live in `searchResources`.
   The bound is a substitution and a clamp, not a rejection: a `limit` at or
   below zero becomes 50 and one above 200 becomes 200.
9. `go/internal/query/infra_search_predicates.go` for `infraSearchHasScope`,
   the rule that 400s a request with no non-blank scope key.

## Invariants

- Keep only infrastructure-search family membership and pure
  argument-to-request selection here. Global route fanout, the private adapter,
  and execution stay in the parent MCP package and `internal/query`.
- Keep the package clause as `package infrasearchtools`; the root imports it
  with an explicit alias.
- Preserve the tool name and its exact method, path, and body keys: `POST`
  `/api/v0/infra/resources/search` with no query string, carrying `query`,
  `category`, `kind`, `provider`, `environment`, `resource_service`,
  `resource_category`, and `limit`, and nothing else.
- Keep every key present even when the caller omitted it, so the handler sees an
  empty filter rather than no field at all.
- Preserve the `limit` default of 50 and send it as a Go `int`. The handler
  would substitute the same 50 for an absent field, so the default is what
  keeps the MCP body's shape identical to the HTTP body's, not what keeps a
  caller from a 400.
- Return the zero request and `handled=false` for unrelated tools, including
  the sibling `count_infra_resources` and `get_infra_resource_inventory`
  aggregates and near-miss names that share the `find_infra` prefix.
- Selection stays pure: no HTTP call, no query, no clock, no environment read.

## Common changes

- Change the route only with the exact child request tests, the root adapter
  parity test, the shared HTTP contract, and applicable golden-corpus proof.
- Change the `limit` default only with the handler's own substitution and the
  advertised schema, because a mismatch changes a client-visible page size.
- Add a filter only after the handler decodes it by name. The handler's request
  struct has no catch-all, so a new key sent from here is inert until
  `searchResources` reads it.
- Add a tool here only after confirming the root `resolveRoute` switch no
  longer answers it, so the two never both claim a name.

## Failure modes

- Importing the MCP root creates a parent-child cycle. Use `routecontract` only.
- Dropping one of the eight keys fails two different ways. The seven scope
  keys are required as a group, so dropping one 400s only the caller whose
  sole scope it was, with `query or structured filter is required`, and
  silently widens every other caller's page. `limit` fails nothing: a lost
  limit hands every caller 50 rows, so a caller who asked for 5 or for 200
  sees a different page with no error. The per-key child and dispatch
  assertions exist because the loud and silent shapes are invisible to a
  single request-level comparison.
- Sending `limit` as a string would change the JSON body's type and make the
  handler's `int` decode fail with 400, where today every non-number collapses
  to the 50-row default.
- Executing a request here would split authorization, timeout, response-budget,
  envelope, summary, and telemetry ownership.
- Claiming a name the root switch still answers makes resolution depend on
  which check runs first.
- Returning a shared map would let one caller's mutation leak into a later
  request.

## Anti-patterns

- Do not add global fanout, HTTP execution, query, storage, authorization,
  summary, or telemetry helpers.
- Do not reintroduce the root `str` and `intOr` helpers; use
  `routecontract.Arguments`, whose coercions they match exactly.
- Do not widen `Route` to a prefix or regular-expression match. Family
  membership is an explicit list of names.

## Verification

From `go/`, run:

```bash
go test ./internal/mcp/infrasearch ./internal/mcp -count=1
go vet ./internal/mcp/infrasearch ./internal/mcp
```

Run `scripts/verify-package-docs.sh` from the repository root. An intentional
MCP query-shape change also requires the golden-corpus gate described by the
parent package.
