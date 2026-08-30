# AGENTS.md — MCP container-image identity route guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` for MCP routing, authorization, and transport rules.
3. `routes.go` and `routes_test.go` for the child request-selection contract.
4. `../dispatch_container_image.go` and
   `../dispatch_container_image_contract_test.go` for the root adapter and the
   production-boundary proof.
5. `../dispatch_repositories.go` for the repository switch this family is
   answered ahead of, and `../dispatch_supply_chain.go` for the six
   supply-chain builders that stayed behind when two of this family's builders
   left it.
6. `../routecontract/README.md` for the dependency-neutral request contract.

## Invariants

- Keep only container-image family membership and pure argument-to-request
  selection here. Global route fanout, the private adapter, and execution stay
  in the parent MCP package and `internal/query`.
- Keep the package clause as `package containerimagetools`; the root imports it
  with an explicit alias.
- Preserve the four tool names and their exact methods, paths, and query keys:
  - `list_container_image_identities` — `GET`
    `/api/v0/supply-chain/container-images/identities` with
    `after_identity_id`, `digest`, `image_ref`, `limit`, `outcome`,
    `repository_id`, `source_repository_id`.
  - `list_container_image_tag_history` — `GET` `/api/v0/images/tag-history`
    with `limit`, `offset`, `repository_id`, `tag`.
  - `count_container_image_identities` — `GET`
    `.../container-images/identities/count` with `digest`, `image_ref`,
    `source_repository_id`, `repository_id`, `outcome`.
  - `get_container_image_identity_inventory` — `GET`
    `.../container-images/identities/inventory` with those five plus
    `group_by`, `limit`, `offset`.
- Keep tag history on `/api/v0/images/tag-history`. It is the only tool here
  that does not share the supply-chain prefix, and `TagHistoryHandler.Mount`
  registers nothing else.
- Keep the count route free of `limit` and `offset`. It answers whole-scope
  totals.
- Keep every key a route owns present even when the caller omitted it, so the
  handler sees an empty filter rather than no filter key at all.
- Preserve the `limit` defaults of 50, 50, and 100, the `offset` defaults of 0,
  and the `group_by` fallback to `outcome`.
- Return the zero request and `handled=false` for unrelated tools, including
  near-miss names that share the `container_image` stem.
- Selection stays pure: no HTTP call, no query, no clock, no environment read.

## Common changes

- Change a route only with the exact child request tests, the root adapter
  parity test, the shared HTTP contract, and applicable golden-corpus proof.
- Change a `limit` default only with the matching handler's own bound check,
  because a mismatch silently changes a client-visible page size.
- Change the identity cursor only with the query layer's keyset predicate.
  The listing pages through `after_identity_id`; tag history and the inventory
  page by `offset`, and the inventory's offset is capped at 10000 by the
  handler.
- Add a tool here only after confirming the root repository switch no longer
  answers it, so the two never both claim a name.

## Failure modes

- Importing the MCP root creates a parent-child cycle. Use `routecontract` only.
- Normalizing the tag-history path onto the sibling prefix selects a path the
  query mux does not serve. This is the likeliest wrong edit in this package.
- Dropping a key fails differently per route, and only two of the four fail
  loudly. On the listing, `limit` is required and a scope anchor is required,
  so losing either 400s — though a scoped token with no grants is answered with
  an empty page before the anchor is checked; losing `after_identity_id` breaks
  keyset paging and re-serves page one. On tag history, `repository_id` and `tag` are both
  required and compose the anchoring `image_ref`, so losing either 400s. On the
  count and the inventory nothing is required, so a lost filter returns 200
  over a wider scope and quietly drops that key from the `scope` block the
  response echoes back. The per-key child and dispatch assertions exist because
  the silent shapes are invisible to a request-level comparison alone.
- Giving the count route a `limit` compiles and reads like a consistency fix
  with its three siblings. The handler never reads it:
  `countContainerImageIdentities` takes no limit and
  `ContainerImageIdentityAggregateFilter` has no field for one, so it caps
  nothing. It advertises a bound the endpoint does not honor, which is worse
  than omitting it.
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
- Do not fold the four query maps into one shared base for the five filters the
  aggregates and the listing have in common. The sets differ by route and the
  difference is the contract.

## Verification

From `go/`, run:

```bash
go test ./internal/mcp/containerimage ./internal/mcp -count=1
go vet ./internal/mcp/containerimage ./internal/mcp
```

Run `scripts/verify-package-docs.sh` from the repository root. An intentional
MCP query-shape change also requires the golden-corpus gate described by the
parent package.
