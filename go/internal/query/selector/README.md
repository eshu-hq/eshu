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

This package owns selector resolution, the selector error types, and
resolved-entity repository-identity hydration. It does not own the graph or
content adapters, the auth context, or any route. It receives ports and an
access filter and answers its questions from those.

## Exported surface

`ResolveExact`, `ResolveExactForAccess`, `ResolveForRequestWithAccess`,
`IsNotFound`, `IsLookupFailure`, `WriteLookupFailure`, `LookupFailureMessage`,
`LooksCanonicalRepositoryID`,
`CatalogMatches`, the `NotFoundError` / `AmbiguousError` / `LookupError`
types, `HydrateResolvedEntityRepoIdentity`, `EntityString`, and
`EntityLabelStrings`. See [doc.go](doc.go).

`LookupError` means a backing read failed, not that the selector was wrong. It
wraps the backend error, so a caller runs `querycontract.WriteGraphReadError`
first (a stale or timed-out guarded reader, or a graph outage or deadline,
answers 503/504) and then answers 500 for whatever `IsLookupFailure` still
reports, with the fixed `LookupFailureMessage` body, never `err.Error()`, which
carries backend text. Only `NotFoundError` is a 404 and only the remaining
selector answers (an ambiguous match) are a 400 (#7626). A caller that maps
selector errors itself calls `WriteLookupFailure` for that 500 step; it writes
the body and the span error and reports false for anything that is not a
lookup failure. When the lookup failed because the caller canceled its own
request, the same step answers 499 (`querycontract.StatusClientClosedRequest`)
with the fixed body instead.

## Dependencies

The Go standard library plus `internal/query/querycontract`, for the
`GraphQuery` and `ContentStore` ports, `RepositoryAccessFilter`, the row-value
decoders, and the HTTP error writers, and `internal/query/tracing`, whose
`WriteServerFailure` writes the lookup-failure answer and marks the request
span.

It is **not** in `querycontract` itself. `ResolveForRequestWithAccess` takes an
`http.ResponseWriter` and writes to it, and request-time orchestration in the
dependency-neutral contract package is exactly what review rejected on the
collector-readiness seam. The same reasoning put the handler span in `tracing`
and the decode error in `decode`.

## Telemetry

This package emits no metric or log of its own and starts no span. Its graph
reads travel through the shared bounded graph-read policy and carry that
policy's `neo4j.query` span; fence and graph-availability failures render
through the shared `WriteGraphReadError` contract.

`ResolveForRequestWithAccess` has no logger, so for the 500 it writes on a
lookup failure it records the error on the span already in the request
context (the caller's handler span, or the `otelhttp` server span) and sets
that span's status to Error with the fixed description
`repository selector lookup failed` (#7626). The description never carries the
selector. When that span is the `otelhttp` server span, `otelhttp` sets Error
again with an empty description once it sees the 5xx, and the SDK keeps the
later description, so in production the span stays Error and keeps the
`exception` event but loses the fixed description. A family that owns a logger
should add its own stage record on top, as the supply-chain security-alert
selector does with `stage_failed`.

A lookup that failed because the caller canceled its own request (the error
wraps `context.Canceled` and the request context is canceled) is not a server
fault. It answers 499, records no `exception` event, leaves the span status
unset, and adds the `eshu.request.client_canceled` event with no attributes.
Both answers come from `tracing.WriteServerFailure`; see the
[tracing README](../tracing/README.md).

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
blind re-pin erases the alarm. The #7626 `LookupError` wrap was re-pinned that
way: the four Cypher fragments of `ResolveExactForAccess` (the two `MATCH`
halves and the two `RETURN` halves around the access splice) hash to
`0a3aacc4546065a32f872400c7f9e268c4d886f5a8f617840fda72e82eeaed54` before and
after.

No-Regression Evidence (#7626): the wrap touches only the failure returns, and
the Cypher bytes are identical, so no backend plan can change. A throwaway
in-package benchmark of `ResolveExactForAccess` against in-memory fakes
(`go test -bench -benchmem -count=8`, darwin/arm64 Apple M5 Max, `benchstat`
of the f93338478 source versus this change) measured: catalog hit 295.7ns to
308.1ns (p=0.083, not significant), 3 allocs and 1.375KiB unchanged; graph hit
306.6ns to 308.5ns (p=0.442), 8 allocs and 1.016KiB unchanged; graph failure
298.3ns to 300.5ns (p=0.328), 8 to 9 allocs and 809B to 825B, the one
`LookupError` value on the error path only.

Observability Evidence (#7626): a lookup failure through
`ResolveForRequestWithAccess` now sets the request span to Error with the fixed
description `repository selector lookup failed` (replaced by `otelhttp`'s empty
5xx description on the server span, see Telemetry) and an `exception` event,
where it previously left no signal at all; the supply-chain security-alert selector
adds a `supply_chain_query.stage_failed` record for stage
`repository_selector_resolve`. Pinned by
`TestResolveForRequestWithAccessMapsLookupFailureTo500` and
`TestRepositorySelectorResolveLookupFailureAnswers500`.

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

## Unclassified resolve errors (#7674)

`ResolveForRequestWithAccess` maps a resolve error in `writeResolveFailure`:
`querycontract.WriteGraphReadError` (503/504), `WriteLookupFailure` (fixed
500, or 499 for a client cancel), a typed `NotFoundError` (404) and a typed
`AmbiguousError` (400), each with its own selector-safe text. Any other error
used to answer 400 with its own text, which could carry backend detail; it
now answers the same fixed `LookupFailureMessage` 500 with the error on the
request span. The exported API is unchanged.

No-Regression Evidence (#7674): the change runs only after resolve has
already returned an error. No Cypher, query parameter, call count, or
success path changed, and `ResolveExactForAccess` (the pinned queryplan
function) is untouched. `go test ./internal/query/... ./internal/queryplan/...
-count=1` covers every importer and exits 0, as does
`go test -race ./internal/query/selector/...`.

Observability Evidence (#7674): an unclassified error now records the
backend error on the request span as an `exception` event and sets status
Error with `LookupFailureMessage`, the same signal a `LookupError` already
gives. `server_failure_test.go` asserts the 404, 400 and 500 answers and the
span.

## Related docs

- [Cypher performance](../../../../docs/public/reference/cypher-performance.md)
- [Package restructure design](../../../../docs/internal/design/package-restructure.md)
