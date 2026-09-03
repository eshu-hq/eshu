# AGENTS.md — MCP infrastructure-inventory route guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` for MCP routing, authorization, and transport rules.
3. `routes.go` and `routes_test.go` for the child request-selection contract.
4. `../dispatch.go` for `resolveRoute`, and
   `../dispatch_infra_resource_aggregates.go` for the private
   `infraInventoryRoute` adapter, consulted as a delegation ahead of the
   switch that held the two inline arms (`investigate_resource`,
   `analyze_infra_relationships`) before the extraction.
5. `../tools_infra_resource_aggregates.go` (for `count_infra_resources` and
   `get_infra_resource_inventory`) and `../ecosystem/tools.go` (for
   `investigate_resource` and `analyze_infra_relationships`) for the four
   advertised schemas. They stay at the parent's root and must keep naming
   the same fields this builder selects.
6. `../routecontract/README.md` for the dependency-neutral request contract.
7. The `go/internal/query` handler behind each path, for the limit/offset
   clamps and required-field checks this package's defaults must stay
   compatible with. Each entry was confirmed with
   `rg --files internal/query -g '<file>'` before being written here:

   | Tool | Handler |
   | --- | --- |
   | `count_infra_resources` | `infra_resource_aggregates_handler.go` |
   | `get_infra_resource_inventory` | `infra_resource_aggregates_handler.go` |
   | `investigate_resource` | `impact_resource_investigation.go` |
   | `analyze_infra_relationships` | `infra_relationship_filter.go` |

## Invariants

- Keep only infrastructure-inventory family membership and pure
  argument-to-request selection here. Global route fanout, the private
  adapter, and execution stay in the parent MCP package and
  `internal/query`.
- Keep the package clause as `package infrainventorytools`; the root imports
  it with an explicit alias.
- Preserve each tool's exact method, path, and body/query keys.
  `count_infra_resources` and `get_infra_resource_inventory` are `GET` with a
  query string and no body; `investigate_resource` and
  `analyze_infra_relationships` are `POST` with a JSON body and no query
  string.
- Keep every selected string key present even when the caller omitted it, so
  the handler sees an empty filter rather than no field at all.
- Preserve the per-tool defaults documented in `README.md`
  (`get_infra_resource_inventory`'s `group_by`/`limit`/`offset`,
  `investigate_resource`'s `max_depth`/`limit`). They are not interchangeable
  across tools.
- Return the zero request and `handled=false` for unrelated tools, including
  `find_infra_resources`, which stays in the sibling `infrasearch` package.
- Selection stays pure: no HTTP call, no query, no clock, no environment
  read.

## Common changes

- Change a route only with the exact child request tests, the root adapter
  parity test (`TestEveryRegisteredToolHasDispatchRoute` in `tools_test.go`),
  the shared HTTP contract, and applicable golden-corpus proof.
- Change a default only against the handler's own behavior and the
  advertised schema, because a mismatch changes a client-visible page size
  or investigation depth.
- Add a body or query key only after the handler decodes it by name; a key
  the handler does not read is inert.
- Add a tool here only after confirming the root `resolveRoute` does not
  also answer it, so the two never both claim a name.

## Failure modes

- Importing the MCP root creates a parent-child cycle. Use `routecontract`
  only.
- A dropped required field is not caught here; the handler decides whether an
  empty string 400s or widens the result. The per-key assertions in
  `routes_test.go` exist because a request-level comparison alone hides which
  key was lost.
- Claiming a name the root also answers makes resolution depend on which
  check runs first — this is why `find_infra_resources` is deliberately
  excluded from `Route`'s switch.
- Executing a request here would split authorization, timeout,
  response-budget, envelope, and telemetry ownership.

## Anti-patterns

- Do not add global fanout, HTTP execution, query, storage, authorization,
  or telemetry helpers.
- Do not reintroduce the root `str`, `intOr`, `boolOr`, or `stringSlice`
  helpers; use `routecontract.Arguments`, whose coercions match them
  exactly.
- Do not widen `Route` to a prefix or regular-expression match. Family
  membership is an explicit list of names.
- Do not pull `find_infra_resources` into this family to "complete" the infra
  set; it owns a request shape and root adapter this package does not share.

## Changes needing ADR review

- Moving registration, dispatch, authorization, or execution into this
  package.
- Changing a tool name, method, path, body/query key, or default in a way
  clients can observe.
- Replacing the explicit name switch with derived membership.
- Adding input validation that returns an error before building a request —
  that would change `Route`'s signature to the three-value
  `(Request, bool, error)` shape `servicecontext` uses, and is a
  client-visible behavior change, not a refactor.

## Verification

From `go/`, run:

```bash
go test ./internal/mcp/infrainventory ./internal/mcp -count=1
go vet ./internal/mcp/infrainventory ./internal/mcp
```

Run `scripts/verify-package-docs.sh` from the repository root. An intentional
MCP query-shape change also requires the golden-corpus gate described by the
parent package.
