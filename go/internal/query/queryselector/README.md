# Repository selector resolution

## Purpose

Turns whatever a client typed into one canonical repository id, or a typed
error saying it matched nothing or matched several. Six properties are accepted
as selectors: id, name, path, local path, remote URL, and slug.

## Ownership boundary

This package owns selector resolution and the two selector error types. It does
not own the graph or content adapters, the auth context, or any route. It
receives ports and an access filter and answers one question.

## Exported surface

`ResolveExact`, `ResolveExactForAccess`, `ResolveForRequestWithAccess`,
`IsNotFound`, `LooksCanonicalRepositoryID`, `CatalogMatches`, and the
`NotFoundError` / `AmbiguousError` types. See [doc.go](doc.go).

## Dependencies

The Go standard library plus `internal/query/querycontract`, for the
`GraphQuery` and `ContentStore` ports, `RepositoryAccessFilter`, the row-value
decoders, and the HTTP error writers.

It is **not** in `querycontract` itself. `ResolveForRequestWithAccess` takes an
`http.ResponseWriter` and writes to it, and request-time orchestration in the
dependency-neutral contract package is exactly what review rejected on the
collector-readiness seam. The same reasoning put the handler span in `queryspan`
and the decode error in `querydecode`.

## Telemetry

No-Observability-Change: this package emits no metric, span, or log of its own.
Its graph reads travel through the shared bounded graph-read policy and carry
that policy's `neo4j.query` span; failures render through the shared
`WriteGraphReadError` contract.

## Gotchas / invariants

**An empty access filter denies rather than matching everything.**
`ResolveExactForAccess` returns `NotFoundError` when `access.Empty()` before it
touches the graph. Inverting that turns a caller with no grants into a caller
who can resolve any repository, which is a tenant-boundary failure that no test
of the happy path would notice.

**Zero rows falls back to a second, unordered query, and that is deliberate.**
The first read orders by id so an ambiguous selector reports deterministically;
the fallback exists for backends that return nothing for the ordered form.
Collapsing the two changes which selectors resolve.

**This callsite is registered in the queryplan manifest.** It carries a
`source_sha256` over its function text, so any edit here — even a rename — fails
the coverage gate until the digest is re-pinned. Re-pin only after proving the
Cypher itself did not change; the manifest exists to catch a query change, and a
blind re-pin erases the alarm.

No-Regression Evidence: the move was proven query-invariant before the digest
was re-pinned. Extracting every string literal per function with `go/parser`
before and after gives identical multisets — `ResolveExactForAccess` 25 literals,
hash unchanged; `CatalogMatches` 2, unchanged; `LooksCanonicalRepositoryID` 3,
unchanged. Only identifiers changed.

## Related docs

- [Cypher performance](../../../../docs/public/reference/cypher-performance.md)
- [Package restructure design](../../../../docs/internal/design/package-restructure.md)
