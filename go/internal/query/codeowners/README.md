# Codeowners ownership family (`go/internal/query/codeowners`)

## Purpose

Owns the `Handler` HTTP surface: `GET /api/v0/codeowners/ownership`, the
bounded graph-backed read of one repository's Phase 3 `DECLARES_CODEOWNER`
edges (issue #5419 Phase 4) plus the `effective_owner` resolved by
`resolveEffectiveRepositoryOwner`.

| Route | Handler |
|---|---|
| `GET /api/v0/codeowners/ownership` | `ListOwnership` (paginated `ownership[]` + `effective_owner` + `next_cursor`) |

The list path runs one bounded graph read per page (three disjoint branch
reads merged into global `(order_index, pattern, owner_ref)` order when a
keyset cursor is present), then resolves `effective_owner` under the same
10s read budget.

## Ownership boundary

This package owns the handler, the ownership row and effective-owner
values crossing it, the paginated/cursor Cypher builders, the
manifest-vs-codeowners precedence resolver, and the capability-support
constructor the family TestMain registers. The service-catalog correlation
port the resolver reads lives in `querycontract` (shared with the staying
service-catalog handler, catalog enrichment, freshness, and
incident-context reads); the Postgres store *implementation* stays in root
package `query` and satisfies the port through root's compatibility
aliases.

Root package `query` keeps the capability matrix row
(`contract_capability_matrix_ext.go`), the OpenAPI path fragments, the
cross-cutting auth and graph-error sweeps, and the
`CodeownersOwnershipHandler` compatibility alias
(`family_codeowners_shim.go`). Root owns the router and always links into
the production binary, so capability registration and the alias live
there. `cmd/api` and `cmd/mcp-server` construct the handler as
`query.CodeownersOwnershipHandler` exactly as before.

## Precedence contract

`resolveEffectiveRepositoryOwner` implements the manifest-vs-codeowners
contract (issue #5419 Phase 4), in order:

1. A service-catalog manifest declaration wins when
   `ListServiceCatalogCorrelations` returns a row for the repository with
   a non-empty `OwnerRef` and an `exact` or `derived` outcome. Other
   outcomes (`ambiguous`, `unresolved`, `stale`, `rejected`) are skipped
   even with a non-empty owner, so a disputed or stale catalog claim never
   outranks a live CODEOWNERS rule.
2. Otherwise the repository's CODEOWNERS rules apply last-match-wins: the
   `DECLARES_CODEOWNER` edge with the highest `order_index`, resolved by
   the dedicated descending `LIMIT 1` read (`CodeownersLastMatchOwnerCypher`,
   never the paginated ascending list).
3. Otherwise the zero-value `EffectiveRepositoryOwner` -- not an error.

A nil correlation store skips step 1 and a nil graph reader skips step 2,
so a partially wired caller still gets whichever branch it can serve.

## Tenant isolation

Both read paths gate on the caller's scoped grant *before* running: a
scoped caller not granted `repository_id` gets the bounded empty page
(empty `ownership`, no `next_cursor`, zero `effective_owner`) without
reading the graph or the correlation store, so `repo-b` data never leaks
to a caller granted only `repo-a` and an ungranted repository reads
exactly like a granted-but-empty one. The in-package
`TestCodeownersOwnershipScopedCallerCannotReadUngrantedRepository` (#5419
Phase 4b) pins all three cases -- denied, granted, unscoped -- against
doubles holding real cross-tenant rows.

## Exported surface

Every export names a staying root caller; see `AGENTS.md` for the
per-symbol list. In brief: the handler (`Handler`, via the root
`CodeownersOwnershipHandler` alias); the row and effective-owner values;
the `EffectiveOwnerSource*` provenance labels; the two Cypher builders and
the graph-query `Cypher` field the staying queryplan production-binding
test pins; and the `OwnershipSupport` capability constructor the family
TestMain registers (root's matrix row should collapse onto it in a
follow-up). See `doc.go` for the godoc-rendered contract.

## Dependencies

Internal packages, all of them leaves that never import root package
`query`:

- `internal/query/querycontract` -- envelopes, capabilities, profiles,
  row-value decoders, repository access filter, graph-error mapping, and
  the shared service-catalog correlation port.
- `internal/query/queryspan` -- handler span plumbing.
- `internal/query/queryauth` -- auth context bounds (tests only: the
  scoped-leak double).

Plus `internal/telemetry` (span names). The tracer and span helper are a
family-local copy of root's `handler_tracing.go` (mirroring
`supplychain/handler_tracing.go`); `handler_tracing_test.go` pins the
copy's emitted span against the queryspan operator contract so drift fails
loudly.

## Telemetry

The route opens one span named `query.codeowners_ownership`
(`telemetry.SpanQueryCodeownersOwnership`) carrying the `http.route` and
`eshu.capability` attributes; each graph call retains the adapter's
`neo4j.execute` dependency span. Span name, capability string, attribute
keys, and the tracer seed (`queryspan.HandlerTracer`) are unchanged by the
move.

## Move evidence (#6060 lane A L2)

This package was created by moving eight files out of root package `query`
(`git mv`, no logic changes) plus the shared service-catalog correlation
port's promotion to `querycontract` (root keeps aliases; see the lane
handoff for why a copy was not an option). The two assertions below are
structural rather than promissory -- each names what a reader can check.

No-Regression Evidence: the move is a package relocation, not a rewrite.
`git diff -M --find-renames` pairs each file with its root predecessor;
the only statement-level changes are the `package` clause, the
`Handler` rename (via the root alias), the `querycontract`/`queryauth`
qualification of helpers root forwards to the identical functions, the
export renames listed in `AGENTS.md`, the family-local tracing copy
documented above, and the queryplan manifest re-keys (file paths, symbol
names, and source digests only -- no Cypher text change). Baseline is the
lane base with green suites; after the move, `go test
./internal/query/codeowners/ -count=1` passes with 0 failures, the staying
root suite pins the routes, contract matrix, and packet parity through the
aliases, and the B-7 golden-corpus gate passes with the B-12 e2e snapshot
byte-identical. Backend/version is unchanged (same NornicDB-first contract
over the same driver path B-7 exercises live), input shape is the family
unit suite plus the golden corpus, and the terminal counts are the
per-package ok plus the corpus checks. The change is safe because behavior
is preserved by construction (a path-only move) and proven by the
unchanged suites and corpus above.

No-Observability-Change: no spans, metrics, structured logs, status
fields, or pprof surface were added, removed, or renamed; the move adds no
new query path, so dashboards and 3 AM triage read exactly as before.

## Gotchas / invariants

- Do not import root package `query`. Root's
  `family_codeowners_shim.go` already imports this package, so the
  reverse import cycles.
- Capabilities are registered in ROOT
  (`contract_capability_matrix_ext.go`), not here -- root owns the router
  and always links into production. This package only declares the
  support constructor the TestMain registers.
- Every read stays bounded (limit clamp + limit+1 probe, 10s budget,
  manifest lookup cap); the handler rejects anchorless reads before any
  store runs.
- The empty page for out-of-grant callers must not read either store:
  probing the stores would let a scoped caller distinguish "out of grant"
  from "granted but empty" and use either path to probe ungranted
  repositories.
- The precedence order is the contract, not an accident of branch order:
  manifest exact/derived first, CODEOWNERS last-match second, zero value
  last. Reordering the branches relabels ownership precedence, and
  nothing errors -- callers compare `effective_owner.source` across
  responses and would simply read the wrong source.

## Related docs

- [HTTP API Reference](../../../../../docs/public/reference/http-api.md)
- [Telemetry](../../../../../docs/public/reference/telemetry/index.md)
- [Architecture](../../../../../docs/public/architecture.md)
