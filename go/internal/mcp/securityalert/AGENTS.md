# AGENTS.md — MCP security-alert reconciliation route guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` for MCP routing, authorization, and transport rules.
3. `routes.go` and `routes_test.go` for the child request-selection contract.
4. `../dispatch_security_alert.go` and
   `../dispatch_security_alert_contract_test.go` for the root adapter and the
   production-boundary proof.
5. `../dispatch_repositories.go` for the repository switch this family is
   answered ahead of, and `../dispatch_supply_chain.go` for the three
   supply-chain builders that stayed there after this family's listing
   builder moved out.
6. `../routecontract/README.md` for the dependency-neutral request contract.

## Invariants

- Keep only security-alert reconciliation family membership and pure
  argument-to-request selection here. Global route fanout, the private
  adapter, and execution stay in the parent MCP package and `internal/query`.
- Keep the package clause as `package securityalerttools`; the root imports
  it with an explicit alias.
- Preserve the three tool names and their exact methods, paths, and query
  keys:
  - `list_security_alert_reconciliations` -- `GET`
    `/api/v0/supply-chain/security-alerts/reconciliations` with
    `after_reconciliation_id`, `cve_id`, `ghsa_id`, `limit`, `package_id`,
    `provider`, `provider_state`, `reconciliation_status`, `repository_id`.
  - `count_security_alert_reconciliations` -- `GET`
    `.../reconciliations/count` with the same filters minus
    `after_reconciliation_id` and `limit`.
  - `get_security_alert_reconciliation_inventory` -- `GET`
    `.../reconciliations/inventory` with those filters plus `group_by`,
    `limit`, and `offset`.
- Keep the count route free of `limit` and `offset`. It answers whole-scope
  totals.
- Keep every key a route owns present even when the caller omitted it, so the
  handler sees an empty filter rather than no filter key at all.
- Preserve the `limit` defaults of 50 and 100, the `offset` default of 0, and
  the `group_by` fallback to `reconciliation_status`.
- Return the zero request and `handled=false` for unrelated tools, including
  near-miss names that share the `security_alert_reconciliation` stem.
- Selection stays pure: no HTTP call, no query, no clock, no environment read.

## Common changes

- Change a route only with the exact child request tests, the root adapter
  parity test, the shared HTTP contract, and applicable golden-corpus proof.
- Change a `limit` default only with the matching handler's own bound check,
  because a mismatch silently changes a client-visible page size.
- Change the listing's cursor only with the query layer's keyset predicate.
  The listing pages through `after_reconciliation_id`; the inventory pages by
  `offset`.
- Add a tool here only after confirming the root repository switch no longer
  answers it, so the two never both claim a name.

## Failure modes

- Importing the MCP root creates a parent-child cycle. Use `routecontract`
  only.
- Dropping a key fails differently per route, and only the listing fails
  loudly. `limit` is required and one of `repository_id`, `provider`,
  `package_id`, `cve_id`, or `ghsa_id` is required as a scope anchor, so
  losing either 400s -- except that an empty scoped-token grant is answered
  with an empty page before the anchor is checked; `provider_state` and
  `reconciliation_status` do not count as anchors on their own. Losing
  `after_reconciliation_id` breaks keyset paging silently and re-serves page
  one. On the count and the inventory nothing is required, so a lost filter
  returns 200 over a wider scope and quietly drops that key from the `scope`
  block the response echoes back.
- Giving the count route a `limit` compiles and reads like a consistency fix
  with the listing and the inventory. The handler never reads it -- it caps
  nothing and advertises a bound the endpoint does not honor.
- Executing a request here would split authorization, timeout,
  response-budget, envelope, summary, and telemetry ownership.
- Claiming a name the repository switch still answers makes resolution
  depend on which check runs first.
- Returning a shared map would let one caller's mutation leak into a later
  request.

## Anti-patterns

- Do not add global fanout, HTTP execution, query, storage, authorization,
  summary, or telemetry helpers.
- Do not reintroduce the root `str` or `intOr` helpers by import; use
  `routecontract.Arguments` for the coercions it already matches.
- Do not widen `Route` to a prefix or regular-expression match. Family
  membership is an explicit list of names.
- Do not fold the two aggregate query maps into the listing's nine-key set.
  The sets differ by route and the difference is the contract.

## Verification

From `go/`, run:

```bash
go test ./internal/mcp/securityalert ./internal/mcp -count=1
go vet ./internal/mcp/securityalert ./internal/mcp
```

Run `scripts/verify-package-docs.sh` from the repository root. An intentional
MCP query-shape change also requires the golden-corpus gate described by the
parent package.
