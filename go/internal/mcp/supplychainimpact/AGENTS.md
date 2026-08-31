# AGENTS.md — MCP supply-chain-impact route guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` for MCP routing, authorization, and transport rules.
3. `routes.go` and `routes_test.go` for the child request-selection contract.
4. `../dispatch_supply_chain_impact.go` and
   `../dispatch_supply_chain_impact_contract_test.go` for the root adapter and
   the production-boundary proof.
5. `../dispatch_repositories.go` for the repository switch this family is
   answered ahead of, and `../dispatch_supply_chain.go` for the four
   supply-chain builders that stayed behind when this family's two
   `dispatch_supply_chain.go` builders left it.
6. `../routecontract/README.md` for the dependency-neutral request contract.

## Invariants

- Keep only supply-chain-impact family membership and pure argument-to-request
  selection here. Global route fanout, the private adapter, and execution stay
  in the parent MCP package and `internal/query`.
- Keep the package clause as `package supplychainimpacttools`; the root
  imports it with an explicit alias.
- Preserve the four tool names and their exact methods, paths, and query keys:
  - `list_supply_chain_impact_findings` — `GET`
    `/api/v0/supply-chain/impact/findings` with `advisory_id`,
    `after_finding_id`, `cve_id`, `ecosystem`, `environment`, `ghsa_id`,
    `image_ref`, `impact_status`, `limit`, `min_priority_score`, `osv_id`,
    `package_id`, `priority_bucket`, `profile`, `repository_id`,
    `service_id`, `severity`, `sort`, `subject_digest`,
    `suppression_state`, `workload_id`, plus `include_suppressed` when set.
  - `count_supply_chain_impact_findings` — `GET`
    `.../impact/findings/count` with the same filters minus
    `after_finding_id`, `limit`, and `sort`.
  - `get_supply_chain_impact_inventory` — `GET` `.../impact/inventory` with
    those filters plus `group_by`, `limit`, and `offset`.
  - `explain_supply_chain_impact` — `GET` `.../impact/explain` with
    `advisory_id`, `cve_id`, `finding_id`, `image_ref`, `package_id`,
    `repository_id`, `service_id`, `subject_digest`, `workload_id`.
- Keep the count route free of `limit` and `offset`. It answers whole-scope
  totals.
- Keep every key a route owns present even when the caller omitted it, so the
  handler sees an empty filter rather than no filter key at all.
- Preserve the `limit` defaults of 50 and 100, the `offset` default of 0, and
  the `group_by` fallback to `impact_status`.
- Preserve the `include_suppressed` three-state contract: absent when unset,
  `"true"`/`"false"` when the caller set an explicit bool, absent for any
  other type. Do not collapse it through `routecontract.Arguments.BoolOr`;
  that loses the "caller never set this" state.
- Return the zero request and `handled=false` for unrelated tools, including
  near-miss names that share the `supply_chain_impact` stem.
- Selection stays pure: no HTTP call, no query, no clock, no environment read.

## Common changes

- Change a route only with the exact child request tests, the root adapter
  parity test, the shared HTTP contract, and applicable golden-corpus proof.
- Change a `limit` default only with the matching handler's own bound check,
  because a mismatch silently changes a client-visible page size.
- Change the finding cursor only with the query layer's keyset predicate. The
  listing pages through `after_finding_id`; the inventory pages by `offset`,
  capped at 10000 by the handler.
- Add a tool here only after confirming the root repository switch no longer
  answers it, so the two never both claim a name.

## Failure modes

- Importing the MCP root creates a parent-child cycle. Use `routecontract`
  only.
- Dropping a key fails differently per route, and only two of the four fail
  loudly. On the listing, `limit` is required and a scope anchor is required,
  so losing either 400s — though a scoped token with no grants is answered
  with an empty page before the anchor is checked; losing `after_finding_id`
  breaks keyset paging and re-serves page one. On the explanation,
  `finding_id` alone or an advisory/CVE anchor plus one bounded scope leg is
  required, so losing the whole scope 400s. On the count and the inventory
  nothing is required, so a lost filter returns 200 over a wider scope and
  quietly drops that key from the `scope` block the response echoes back. The
  per-key child and dispatch assertions exist because the silent shapes are
  invisible to a request-level comparison alone.
- Giving the count route a `limit` compiles and reads like a consistency fix
  with the listing and the inventory. The handler never reads it —
  `countImpactFindings` takes no limit — so it caps nothing. It advertises a
  bound the endpoint does not honor, which is worse than omitting it.
- Collapsing `include_suppressed` through `BoolOr` turns "the caller never set
  this" into an explicit `false`, which is not the same wire value the
  handler's own default produces when the key is absent entirely.
- Executing a request here would split authorization, timeout, response-budget,
  envelope, summary, and telemetry ownership.
- Claiming a name the repository switch still answers makes resolution depend
  on which check runs first.
- Returning a shared map would let one caller's mutation leak into a later
  request.

## Anti-patterns

- Do not add global fanout, HTTP execution, query, storage, authorization,
  summary, or telemetry helpers.
- Do not reintroduce the root `str`, `intOr`, or `boolStr` helpers by import;
  use `routecontract.Arguments` for coercions it already matches, and keep the
  local `boolStr` here — it is a deliberate reimplementation, not a shortcut,
  because `routecontract` has no equivalent tri-state helper.
- Do not widen `Route` to a prefix or regular-expression match. Family
  membership is an explicit list of names.
- Do not fold the four query maps into one shared base for the eighteen
  filters the count and the inventory have in common with the listing. The
  sets differ by route and the difference is the contract.

## Verification

From `go/`, run:

```bash
go test ./internal/mcp/supplychainimpact ./internal/mcp -count=1
go vet ./internal/mcp/supplychainimpact ./internal/mcp
```

Run `scripts/verify-package-docs.sh` from the repository root. An intentional
MCP query-shape change also requires the golden-corpus gate described by the
parent package.
