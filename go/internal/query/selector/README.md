# Repository selector resolution

## Purpose

Turns whatever a client typed into one canonical repository id, or a typed
error saying it matched nothing or matched several. Six properties are accepted
as selectors: id, name, path, local path, remote URL, and slug.

`HydrateResolvedEntityRepoIdentity` answers a related but different question:
given an entity the caller already resolved, fill in ITS OWN canonical
repository identity (repo_id, repo_name) under the same access filter, from
graph projection, the content catalog, or a bounded workload-backfill graph
read, in that order.

## Ownership boundary

This package owns selector resolution, the two selector error types, and
resolved-entity repository-identity hydration. It does not own the graph or
content adapters, the auth context, or any route. It receives ports and an
access filter and answers its questions from those.

## Exported surface

`ResolveExact`, `ResolveExactForAccess`, `ResolveForRequestWithAccess`,
`IsNotFound`, `LooksCanonicalRepositoryID`, `CatalogMatches`, the
`NotFoundError` / `AmbiguousError` types, `HydrateResolvedEntityRepoIdentity`,
`EntityString`, and `EntityLabelStrings`. See [doc.go](doc.go).

## Dependencies

The Go standard library plus `internal/query/querycontract`, for the
`GraphQuery` and `ContentStore` ports, `RepositoryAccessFilter`, the row-value
decoders, and the HTTP error writers.

It is **not** in `querycontract` itself. `ResolveForRequestWithAccess` takes an
`http.ResponseWriter` and writes to it, and request-time orchestration in the
dependency-neutral contract package is exactly what review rejected on the
collector-readiness seam. The same reasoning put the handler span in `tracing`
and the decode error in `decode`.

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

**`HydrateResolvedEntityRepoIdentity` scrubs a leaked backend projection
placeholder before doing anything else (#6408).** A backend bug can return its
own unresolved expression text (e.g. `"coalesce(repo.id, repoViaInstance.id)"`)
as if it were a real `repo_id`/`repo_name` value; the scrub
(`entity.ClearResolvedEntityRepoProjectionPlaceholders`) clears exactly
those four known shapes before any other hydration path runs, so a still-open
backend bug never gets treated as resolved identity.

**Hydration attaches only a repository the caller is granted, which is a
different rule from workload admission (#6786 review).** The workload context
route admits a Workload through `querycontract.WorkloadGrantAdmitted`: its own
`repo_id` is granted, or a granted repository `DEFINES` it. Hydration does not
use that rule, on purpose. It does not decide whether the caller may see the
entity; the entity already came from a grant-checked read. It decides which
repository to attach, and it re-checks that repository with
`AllowsRepositoryID`. `WorkloadGrantAdmitted` would admit the row whenever the
workload's own `repo_id` is granted, and would then attach an ungranted
`DEFINES` repository's id and name. The cost of the stricter rule:
`GetEntityContext` takes the repository from `CONTAINS`/`DEFINES` and never
reads `w.repo_id`, so a Workload whose own `repo_id` is granted but which no
granted repository `DEFINES` is not found on `/entities/{id}/context` (fail
closed), while `/workloads/{id}/context` admits it. That trade is deliberate:
a missing context is preferable to attaching an ungranted repository's
identity.
`TestHydrateResolvedEntityRepoIdentityDoesNotUseWorkloadAdmission` pins this.

## Related docs

- [Cypher performance](../../../../docs/public/reference/cypher-performance.md)
- [Package restructure design](../../../../docs/internal/design/package-restructure.md)
