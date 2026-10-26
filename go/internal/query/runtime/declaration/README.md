# Runtime declaration family (`go/internal/query/runtime/declaration`)

## Purpose

Owns the `Handler` HTTP surface: `POST /api/v0/runtime-declarations/query`,
the bounded graph-backed read of the `RuntimeDeclaration` nodes the
`runtime_declaration_projection` reducer domain writes (#8816), plus the
`runtime_declarations` section of `get_repo_context` served through
`LoadSection`.

| Route | Handler |
|---|---|
| `POST /api/v0/runtime-declarations/query` | `listDeclarations` (paged `declarations[]` + `excluded_from_comparison` + `next_cursor`) |

The list path runs up to three bounded graph reads per page (the page, the
exclusion count when a comparison is requested, and the repository-name
lookup that degrades to `partial_reasons` instead of failing), then answers
with the truth envelope. It answers questions such as "which repositories
declare a Node major below 20" for the `list_runtime_version_declarations`
MCP tool.

## Ownership boundary

This package owns the handler, the declaration row values crossing it, the
validated request shape, the paginated/cursor Cypher builders, the
exclusion summarizer, the context section loader, and the capability-support
constructor the family TestMain registers. Root package `query` keeps the
capability matrix row (`contract/capability_matrix.go`), the OpenAPI path
fragment (`openapi/paths/repository/runtime_declarations.go`), the
cross-cutting auth and graph-error sweeps, and the router mount
(`handler.go`, `auth_scoped_routes.go`). Root owns the router and always
links into the production binary, so capability registration lives there.
`cmd/api` and `cmd/mcp-server` construct the handler directly.

## Comparison contract

`major_below` and `version_below` compare the declared version within one
runtime (runtime is required: Node 20 and Python 20 are unrelated) and return
only the declarations the comparison can place. Left out and named in
`partial_reasons`, with counts in `excluded_from_comparison.by_spec_kind`:

- `range`, `alias`, `unresolved` specs -- a version the declaration never
  resolves to (a range, `lts/*`, `${NODE_VERSION}`) is never guessed.
- `partial` specs too imprecise for the bound (`20` against `20.10`).
- `other` for a spec kind this build does not know, so a new kind is
  counted, never dropped.

## Tenant isolation

Both read paths gate on the caller's scoped grant *before* running: a
scoped caller with an empty grant gets the bounded empty page (empty
`declarations`, no `next_cursor`) without reading the graph, and `repo_ids`
narrows within the grant rather than around it, so `repo-b` data never
leaks to a caller granted only `repo-a` and an ungranted repository reads
exactly like a granted-but-empty one. The in-package `access_test.go` pins
denied, granted, and unscoped callers against doubles holding real
cross-tenant rows.

## Exported surface

- `Handler` -- root `handler.go` field, `cmd/api` and `cmd/mcp-server`
  wiring, staying root tests.
- `LoadSection`, `Section`, `RuntimeSummary` -- the repository context
  section (`repository/context.go`).
- `Support`, `Capability` -- the capability id and constructor the family
  TestMain registers and root's matrix row calls.

See `doc.go` for the godoc-rendered contract.

## Dependencies

Internal packages, all of them leaves that never import root package
`query`:

- `internal/query/querycontract` -- envelopes, capabilities, profiles,
  row-value decoders, repository access filter, and graph-error mapping.
- `internal/query/repository/readmodel` -- repository display names.
- `internal/query/tracing` -- handler span plumbing.
- `internal/telemetry` -- span names and the query instruments.

The tracer and span helper are a family-local copy of root's
`handler_tracing.go` (mirroring `supply/chain/handler_tracing.go`);
`handler_tracing_test.go` pins the copy's emitted span against the tracing
package's operator contract so drift fails loudly.

## Telemetry

The route opens one span named `query.runtime_declarations`
(`telemetry.SpanQueryRuntimeDeclarations`) carrying the `http.route` and
`eshu.capability` attributes; each graph call retains the adapter's
`neo4j.execute` dependency span. Two instruments are registered in
`internal/telemetry/instruments_runtime_declaration_query.go`:

- `eshu_dp_runtime_declaration_query_duration_seconds` -- graph read
  duration by closed `operation` (`list`, `excluded_count`,
  `repository_names`, `context_section`) and `outcome` (`ok`, `error`).
- `eshu_dp_runtime_declaration_query_excluded_total` -- declarations a
  comparison could not place, by closed `reason` (`range`, `alias`,
  `unresolved`, `partial`, `other`).

Neither carries a repository id or runtime name. The measured p95 against
the capability budget is recorded in
`docs/internal/evidence/8817-runtime-declaration-query.md`.

## Failed reads (#7674)

A failed graph read answers a fixed message per step (`runtime declaration
graph read failed`, `runtime declaration comparison exclusion read
failed`), never the backend error text. Each step runs
`querycontract.WriteGraphReadError` first (fence or graph-availability
verdicts answer 503/504), then `tracing.WriteServerFailure`: `500` with the
error on the handler span, or `499` when the caller canceled the request.
`server_failure_test.go` covers both steps.

## Gotchas / invariants

- Do not import root package `query`. Root's handler owns the router and
  imports this package for its handler field, so the reverse import cycles.
- Capabilities are registered in ROOT (`contract/capability_matrix.go`),
  not here. This package only declares the `Support` constructor the
  TestMain registers.
- Every read stays bounded (limit clamp + limit+1 probe, 10s budget,
  at-most-100 repo_ids, keyset cursor). Widening a bound changes the
  queryplan-pinned page shapes in
  `go/internal/queryplan/testdata/source-coverage/second.yaml`.
- The empty page for out-of-grant callers must not read the graph: probing
  would let a scoped caller distinguish "out of grant" from "granted but
  empty".
- A version comparison is only meaningful inside one runtime; the handler
  rejects `major_below`/`version_below` without `runtime` with a 400.
- The registration must stay a literal
  `mux.HandleFunc("POST /api/v0/runtime-declarations/query",
  h.listDeclarations)`: the #5584 route-serves-data registry gate matches
  the literal, and `query/codeowners/ownership.go` is the precedent.
- Unknown request fields are rejected, not ignored: a misspelled filter
  would otherwise answer with every declaration, which reads as a clean
  fleet.

## Related docs

- [HTTP API Reference](../../../../../docs/public/reference/http-api.md)
- [Telemetry](../../../../../docs/public/reference/telemetry/index.md)
- [Architecture](../../../../../docs/public/architecture.md)
