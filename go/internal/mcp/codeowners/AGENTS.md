# AGENTS.md — MCP CODEOWNERS ownership route guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` for MCP routing, authorization, and transport rules.
3. `routes.go` and `routes_test.go` for the child request-selection contract.
4. `../dispatch_codeowners.go` and `../dispatch_codeowners_contract_test.go` for
   the root adapter and the production-boundary proof.
5. `../dispatch_repositories.go` for the repository switch this family is
   answered ahead of.
6. `../routecontract/README.md` for the dependency-neutral request contract.

## Invariants

- Keep only CODEOWNERS family membership and pure argument-to-request selection
  here. Global route fanout, the private adapter, and execution stay in the
  parent MCP package and `internal/query`.
- Keep the package clause as `package codeownerstools`; the root imports it with
  an explicit alias.
- Preserve the tool name and its exact method, path, and query keys: `GET`
  `/api/v0/codeowners/ownership` with no body, carrying `repository_id`,
  `limit`, `after_order_index`, `after_pattern`, and `after_ref` and nothing
  else.
- Preserve the `limit` default of 50.
- Preserve `optionalIntString`. An absent `after_order_index` must format as the
  empty string, never as `"0"`. It is one leg of a three-part keyset cursor the
  handler accepts only whole, so a default would make a first page look like a
  seek past order index zero. A present-but-wrong-typed value still formats as
  `"0"`; that is the coercion of a leg the caller did send, not a default.
- Return the zero request and `handled=false` for unrelated tools, including
  near-miss names that share the `list_codeowners` prefix.
- Selection stays pure: no HTTP call, no query, no clock, no environment read.

## Common changes

- Change the route only with the exact child request tests, the root adapter
  parity test, the shared HTTP contract, and applicable golden-corpus proof.
- Change the `limit` default only with the handler's own bound check, because a
  mismatch silently changes a client-visible page size.
- Change the cursor only with the query layer's keyset predicate. The three legs
  are one unit; dropping or defaulting a leg here silently reorders or skips a
  caller's page rather than failing.
- Add a tool here only after confirming the root repository switch no longer
  answers it, so the two never both claim a name.

## Failure modes

- Importing the MCP root creates a parent-child cycle. Use `routecontract` only.
- Replacing `optionalIntString` with `IntOr(key, 0)` compiles, passes a casual
  read, and corrupts every uncursored first page. The child and root cursor
  tests exist to catch exactly that edit.
- Executing a request here would split authorization, timeout, response-budget,
  envelope, summary, and telemetry ownership.
- Claiming a name the repository switch still answers makes resolution depend on
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
go test ./internal/mcp/codeowners ./internal/mcp -count=1
go vet ./internal/mcp/codeowners ./internal/mcp
```

Run `scripts/verify-package-docs.sh` from the repository root. An intentional
MCP query-shape change also requires the golden-corpus gate described by the
parent package.
