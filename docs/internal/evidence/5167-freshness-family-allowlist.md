# Freshness delta family: off the pending ledger, onto the scoped allowlist

The three `freshness` delta routes were the last `freshness` entries in
`pendingRowFilteringRoutes`
(`go/internal/query/auth_scoped_routes_pending_row_filtering.go`), the #5167
Group B ledger of MCP-reachable routes that bind no caller grant and therefore
answer a scoped or browser-session caller with a middleware 403. This document
covers both commits that empty that family:

1. `GET /api/v0/freshness/changed-since` and `GET /api/v0/freshness/generations`
   -- both handlers already bound the grant in their shipped SQL, so the commit
   was steps 2-4 of the ledger header's own removal procedure: a matcher, the
   ledger move, and the OpenAPI marker. No handler or SQL logic was touched.
2. `GET /api/v0/freshness/services/changed-since` -- this one needed the
   binding built first, because its tables carry no repository or scope column
   at all. See [The service route](#the-service-route) below.

## Commit 1: what moved

- `scopedFreshnessDeltaRoute` (`auth_scoped_routes_status.go`) matches exactly
  the two `GET` paths and is wired into
  `scopedHTTPRouteSupportsTenantFilter` next to
  `scopedFreshnessCausalityRoute`.
- Both routes move from `pendingRowFilteringRoutes` into
  `scopedTokenAdvertisedRoutes` with the explicit class
  `scopedRouteGrantBound`.
- Both OpenAPI operations gain `"x-scoped-token-support": true` and one
  sentence of prose: "Scoped tokens receive only granted repositories and
  scopes; an ungranted selector returns not-found." The same sentence is added
  to the two MCP tool descriptions (`get_changed_since`,
  `get_generation_lifecycle`) and to the two route sections of
  `docs/public/reference/http-api/status-admin.md`.
- `TestToolsPreserveFreshnessRegistrationContract`
  (`go/internal/mcp/freshness/tools_test.go`) pins a SHA-256 over the marshalled
  freshness tool definitions, so the description edit moves that pin from
  `eaa37368...` to `dd7c7265...`. The pin exists to make a tool-contract change
  deliberate rather than accidental; this one is deliberate and is the only
  artifact the description edit touches. No cassette, no B-12 snapshot entry,
  and neither generated capability-catalog file carries tool or operation
  description text (`catalog.generated.json` and
  `surface-inventory.generated.json` record `{category, name, readiness}`
  only), so nothing is regenerated here.

## Commit 1: why it is safe

The grant is bound in the shipped SQL, on the resolved row rather than on the
selector the caller typed:

- `go/internal/storage/postgres/changed_since_sql.go:49-51` --
  `($3::boolean = false OR (scope.scope_kind = 'repository' AND
  scope.source_key = ANY($4)) OR scope.scope_id = ANY($5))`.
- `go/internal/storage/postgres/generation_lifecycle_sql.go:114-116` --
  `($8::boolean = false OR (scope.scope_kind = 'repository' AND
  scope.source_key = ANY($9)) OR generation.scope_id = ANY($10))`.

`TestFreshnessGrantPredicatesArePresentInTheShippedSQL`
(`internal/storage/postgres`) pins both predicate texts, so a rewrite that drops
either arm fails before it reaches a caller. An ungranted selector resolves to
no row and is served as the route's ordinary not-found, which keeps the route
from working as an existence oracle for another tenant's scopes or generations.

The class is `scopedRouteGrantBound`, not identity-bound or tenant-data-free.
That keeps an all-scope browser session behind the `BrowserSessionRoutePolicy`
mode check (#6450): an all-scope caller's
`RepositoryAccessFilterFromContext` is not `Scoped()`, so these predicates go
inert for it, and the hosted multi-tenant posture must refuse rather than answer
from the whole graph. A restricted session, whose grant the queries can bind,
is admitted.

## Commit 1: tests run

Run from `go/` with `GOCACHE` isolated to this worktree; exit codes captured
directly, after the last edit.

| Command | Exit |
| --- | --- |
| `go test ./internal/query -count=1` | 0 |
| `go test ./internal/mcp ./cmd/api -count=1` | 0 |
| `go test ./internal/mcp/... -count=1` | 0 |
| `go test ./internal/storage/postgres -run 'PresentInTheShippedSQL\|PassesGrantToQuery' -count=1` | 0 |
| `go vet ./internal/query ./internal/mcp` | 0 |

`TestChangedSinceTwoTenantGrantBoundary`
(`internal/query/freshness_changed_since_two_tenant_test.go`) is new. It drives
the production handler against a fake reader that applies the same intersection
`resolveChangedSinceScopeQuery` applies, over two repository-kind scopes owned
by different tenants: the granted repository returns its delta, the ungranted
one returns a not-found byte-identical to an absent repository once the caller's
own echoed selector is normalized, and a shared-key caller still sees both.
It mirrors `TestGenerationLifecycleTwoTenantGrantBoundary`, the equivalent proof
for the sibling route.

The new test passes on arrival, so its value rests on failing when the
production path is broken rather than on a first red run. Both directions were
exercised by mutating `listChangedSince`'s single grant assignment:

| Mutation in `freshness_changed_since.go` | Result | Exit |
| --- | --- | --- |
| `filter.Scoped = false` (grant never bound) | `out of grant is not found` fails: status 200 with `scope-b`'s delta | 1 |
| `filter.Scoped = true` (bound for every caller) | `all scope shared key sees both tenants` fails: 404 for both repositories | 1 |
| `filter.Scoped = access.Scoped()` (shipped) | pass | 0 |

## Commit 1 BITES: the allowlist wiring

Removing the `scopedFreshnessDeltaRoute` clause from
`scopedHTTPRouteSupportsTenantFilter`, leaving the ledger entries and OpenAPI
markers in place, reproduces the #5150 advertised-but-unwired shape:

```
$ go test ./internal/query \
    -run 'TestScopedTokenAllowlistCompleteness|TestAuthMiddlewareAllScopesBrowserSessionSplitAcrossLedger' \
    -count=1 > /tmp/prA-bites-red.log 2>&1; echo "EXIT=$?"
EXIT=1
--- FAIL: TestScopedTokenAllowlistCompleteness (0.02s)
    GET /api/v0/freshness/changed-since: OpenAPI path entry carries a tenant-scope
      marker, but scopedHTTPRouteSupportsTenantFilter(r) returns false
    GET /api/v0/freshness/generations: OpenAPI path entry carries a tenant-scope
      marker, but scopedHTTPRouteSupportsTenantFilter(r) returns false
--- FAIL: TestAuthMiddlewareAllScopesBrowserSessionSplitAcrossLedger
    .../a_grant_bearing_scoped_bearer/GET_/api/v0/freshness/changed-since:
      handler called = false, want true; status = 403
    .../a_grant_bearing_scoped_bearer/GET_/api/v0/freshness/generations:
      handler called = false, want true; status = 403
    .../d_restricted_browser_session/GET_/api/v0/freshness/changed-since:
      handler called = false, want true; status = 403
    .../d_restricted_browser_session/GET_/api/v0/freshness/generations:
      handler called = false, want true; status = 403
```

Restoring the clause:

```
$ go test ./internal/query \
    -run 'TestScopedTokenAllowlistCompleteness|TestAuthMiddlewareAllScopesBrowserSessionSplitAcrossLedger' \
    -count=1 > /tmp/prA-bites-green.log 2>&1; echo "EXIT=$?"
EXIT=0
ok  	github.com/eshu-hq/eshu/go/internal/query	1.781s
```

The split table covers both new ledger entries automatically, under all six
caller shapes, because it iterates `scopedTokenAdvertisedRoutes`.

## Commit 1: markers

No-Regression Evidence: no query shape changed. No SQL string, bind argument,
join, or index is touched, so the plan cache key and the wire text of both
queries are byte-identical to `origin/main`. On the admission path the change
adds one method comparison plus at most two string comparisons per request, in
a function that already performs dozens of the same shape before reaching the
freshness clause, and only for requests that get past the shared-key-only and
allowlist checks ahead of it. No before/after wall-clock claim is made: there
is no measured hot path here to compare, and a package test time is not one.
`go test ./internal/query -count=1` took 4.788s green after the last edit,
recorded as a smoke figure rather than as a delta.

No-Observability-Change: no span, metric, or log key is added or removed. The
two handler spans keep their existing names and attributes,
`query.freshness_changed_since` and `query.freshness_generation_lifecycle`. A
refusal on these routes continues to emit the governance-audit event the
scoped-route admission path already emits --
`governanceaudit.EventTypeReadAuthorization` with
`governanceaudit.DecisionDenied` -- and the reason code an operator reads for it
changes meaning rather than appearing: before this change a scoped or
browser-session caller on either route was refused with
`scoped_route_not_enabled` (the route had no scoped authorization at all);
after it, a grant-bearing caller is admitted and only an all-scope browser
session under a fail-closed `BrowserSessionRoutePolicy` is refused, with
`scoped_route_all_scope_grant_required`. That is the #6450 code, already
defined and already asserted by the split table; this change adds two routes to
the population that can emit it.

---

## The service route

`GET /api/v0/freshness/services/changed-since` could not reuse commit 1's
shape. `schema/data-plane/postgres/025_service_materialization_generations.sql`
and `026_service_evidence_snapshots.sql` carry `service_id` and generation
columns only -- no repository, no scope, nothing joining to `ingestion_scopes`
-- so there is no column for a grant predicate to bind. Every read in
`go/internal/storage/postgres/service_changed_since_sql.go` filters on
`service_id` / `generation_id` alone. Left unbound, promoting the route would
have handed an ungranted caller another tenant's service existence, its current
and prior generation ids and timestamps, per-family added/updated/retired/
superseded counts, and bounded `service_evidence_key` samples, which embed
owner refs, deployment identities, and incident identities.

The route's `service_id` is also not the identifier the four already-allowlisted
service routes resolve. Those resolve a graph `Workload` node id or name
(`service_workload_resolution.go`); this one takes the catalog entity ref the
reducer wrote as `decision.ServiceID` -- for example
`component:default/deployable-config`, the value
`go/internal/reducer/service_changed_since_golden_fixture_test.go` pins. So
`resolveServiceWorkloadCandidate` would have resolved nothing for a real input,
and reusing it would have been a binding in name only.

The reducer did know the owning repository when it wrote the generation
(`serviceRepositoryIndex`, `service_catalog_correlation.go:333-346`) but
discards it. The same decision set also writes the
`reducer_service_catalog_correlation` facts, whose payload carries
`repository_id`, `service_id`, and `candidate_repository_ids` on one row, and
those are read through `query.ServiceCatalogCorrelationStore`, whose filter
already has both a `ServiceID` selector and the grant arrays that
`GET /api/v0/service-catalog/correlations` (itself `scopedRouteGrantBound`)
binds. That is the mapping this change uses.

### What the binding does

`FreshnessHandler` gains one field, `ServiceOwnership
ServiceCatalogCorrelationStore`, wired at both construction sites
(`go/cmd/api/wiring_handlers.go`, `go/cmd/mcp-server/wiring_router.go`) with
the store both entrypoints already build for other handlers. No new store, no
new SQL, no new join.

`FreshnessHandler.serviceChangedSinceGrantAdmits`
(`freshness_service_changed_since.go`) runs between the nil-reader guard and
the lineage read:

- Unscoped caller (shared key, admin, local): returns immediately. No extra
  query, no behaviour change.
- Scoped caller with an empty grant: refuses without touching any store. This
  short circuit is load-bearing, not belt-and-braces --
  `listServiceCatalogCorrelationsQuery`'s grant clause is
  `(cardinality($13)=0 AND cardinality($14)=0) OR ...`, so an empty grant makes
  the whole disjunction TRUE and the store would hand back every tenant's row.
  `service_catalog.go:111-113` treats it the same way.
- Scoped caller with a grant: one `ListServiceCatalogCorrelations` call with
  the service id and the caller's two grant arrays, `Limit: 1`. Zero rows
  refuses. One row admits.
- Scoped caller with `ServiceOwnership` nil: refuses. A deployment that cannot
  resolve ownership must not answer instead of resolving it.

Every refusal goes through `writeServiceChangedSinceNotFound`, the existing
unknown-service block factored into a helper, so the ungranted answer is
byte-identical to the unknown-service answer and the route is not an existence
oracle.

Two consequences are deliberate and documented on the handler:

- A generation can outlive its correlation fact. The correlation query requires
  the fact's generation to still be its scope's active one, so removing a
  catalog entity leaves a scoped caller with not-found for a service whose
  lineage rows still exist. Fail-closed and correct: with the catalog entity
  gone there is no evidence of ownership left, and an unowned service must not
  be readable by a scoped caller. An unscoped operator still sees it.
- A correlation row with an empty `repository_id` (an `unresolved`,
  `provenance_only`, or `ambiguous` decision) is admitted only through
  `candidate_repository_ids ?|` or `scope_id = ANY(...)`. If neither matches,
  the scoped caller is denied, which matches the deny-by-default rule for an
  unbindable row in `impact_access_filter.go:28-35`.

### The ledger move

`scopedFreshnessDeltaRoute` grows the third path and its doc comment now
explains why this route's binding lives in the handler while its two siblings'
live in SQL. The route moves out of `pendingRowFilteringRoutes` and into
`scopedTokenAdvertisedRoutes` as `scopedRouteGrantBound`. The OpenAPI operation
gains `"x-scoped-token-support": true` and one sentence; the same sentence goes
on the `get_service_changed_since` tool description and the route's section of
`docs/public/reference/http-api/status-admin.md`.
`TestToolsPreserveFreshnessRegistrationContract` pins a SHA-256 over the
marshalled freshness tool definitions, so that sentence moves the pin from
`dd7c7265...` to `197bfde6...`. That pin exists to make a tool-contract change
deliberate; this one is. No cassette and no B-12 snapshot entry carries tool or
operation description text, so nothing is regenerated.

`cmd/mcp-server/wiring_test.go` gains an explicit
`router.Freshness.ServiceOwnership == nil` assertion. The reflective sweeps
(`TestNewRouterWiresEveryFieldOrDocumentsWhyNot` and its mcp-server twin)
already catch a nil interface field one level inside a wired handler, so a
half-wired entrypoint fails on both binaries; the named assertion is there
because a nil resolver would present as "this tenant has no services" rather
than as a wiring bug.

### The service route: red, then green

The test is `TestServiceChangedSinceTwoTenantGrantBoundary`
(`internal/query/freshness_service_changed_since_grant_test.go`). Its ownership
fake applies the same `service_id` and grant intersection
`listServiceCatalogCorrelationsQuery` applies, including the empty-arrays-are-
permissive arm, so a handler that stops passing the grant resolves the other
tenant's service here exactly as it would in Postgres. Both fakes carry a
`touched` flag, because the binding for this route is a refusal *before* the
lineage read and a status code alone cannot prove that.

Written first, with only the struct field added and no enforcement:

```
$ go test ./internal/query -run 'TestServiceChangedSinceTwoTenantGrantBoundary' \
    -count=1 > <scratchpad>/prA2-red.log 2>&1; echo "EXIT=$?"
EXIT=1
--- FAIL: TestServiceChangedSinceTwoTenantGrantBoundary
    --- FAIL: .../in_grant_returns_the_delta
        ownership store was never consulted for a scoped caller; the grant is
        not bound
    --- FAIL: .../out_of_grant_is_not_found_before_the_lineage_read
        status = 200, want 404; an ungranted service must not resolve; body =
        {"data":{...,"current_active_generation_id":"gen-current-repo-b",
        "service_id":"component:default/tenant-b",...}}
    --- FAIL: .../empty_grant_touches_neither_store
        status = 200, want 404; a scoped caller with no grant must resolve
        nothing
    --- FAIL: .../scoped_caller_fails_closed_when_ownership_is_unwired
        status = 200, want 404; an unwired ownership store must fail closed for
        a scoped caller
```

That red is the defect itself: tenant A's token reading tenant B's service
lineage, 200, with tenant B's current generation id in the body. Green after
`serviceChangedSinceGrantAdmits` landed:

```
$ go test ./internal/query -run 'TestServiceChangedSinceTwoTenantGrantBoundary' \
    -count=1; echo "EXIT=$?"
ok  	github.com/eshu-hq/eshu/go/internal/query	1.780s
EXIT=0
```

### The service route: BITES

Each mutation was applied alone to `serviceChangedSinceGrantAdmits`, run, and
reverted. Exit codes captured directly.

| Mutation | Case that fails | Exit |
| --- | --- | --- |
| Ownership refusal neutered (`len(rows) == 0` branch dropped) | `out of grant is not found before the lineage read`: 200 with `gen-current-repo-b` | 1 |
| `access.Empty()` short circuit dropped | `empty grant touches neither store`: 200 with tenant A's delta, because the store's grant clause is TRUE on empty arrays | 1 |
| `!access.Scoped()` early return dropped (bind unconditionally) | `shared key never consults the ownership store`: the store is consulted for an unscoped caller on both services | 1 |
| `ServiceOwnership == nil` opens instead of refusing | `scoped caller fails closed when ownership is unwired`: 200 | 1 |
| Shipped code | pass | 0 |

The third row is the one a deny-only test cannot see: binding unconditionally
still answers 200 for a shared key here, and only the `touched` flag catches
that the unscoped path grew a query it does not need -- and that an unscoped
operator would lose any service whose catalog entity has since been removed.

Removing the third path from `scopedFreshnessDeltaRoute` while leaving the
ledger row and the OpenAPI marker reproduces the #5150 advertised-but-unwired
shape:

```
$ go test ./internal/query \
    -run 'TestScopedTokenAllowlistCompleteness|TestAuthMiddlewareAllScopesBrowserSessionSplitAcrossLedger' \
    -count=1 > /tmp/bites-E.log 2>&1; echo "EXIT=$?"
EXIT=1
--- FAIL: TestScopedTokenAllowlistCompleteness
    GET /api/v0/freshness/services/changed-since: OpenAPI path entry carries a
      tenant-scope marker, but scopedHTTPRouteSupportsTenantFilter(r) returns
      false
--- FAIL: TestAuthMiddlewareAllScopesBrowserSessionSplitAcrossLedger
    .../a_grant_bearing_scoped_bearer/GET_/api/v0/freshness/services/changed-since:
      handler called = false, want true; status = 403
    .../d_restricted_browser_session/GET_/api/v0/freshness/services/changed-since:
      handler called = false, want true; status = 403
```

Restored, the same run is `EXIT=0`.

## Commit 2: markers

No-Regression Evidence: no existing query shape changed. No SQL string, bind
argument, join, or index is touched; the ownership resolution reuses
`listServiceCatalogCorrelationsQuery` verbatim, so its plan cache key and wire
text are byte-identical to `origin/main`. The added cost is one bounded
`SELECT ... LIMIT 1` on `fact_records`, anchored by the `service_id` payload
predicate and the caller's grant arrays, and it runs only on the scoped path:
an unscoped caller adds nothing (`shared key never consults the ownership
store` pins that), and a scoped caller with no grant adds nothing (`empty grant
touches neither store` pins that). No before/after wall-clock claim is made:
there is no measured hot path here to compare, and a package test time is not
one. `go test ./internal/query -count=1` took 4.887s green after the last edit,
recorded as a smoke figure rather than as a delta.

No-Observability-Change: no span, metric, or log key is added or removed. The
handler span keeps its existing name and attributes,
`query.freshness_service_changed_since`; a pre-read refusal returns before
`span.SetAttributes`, exactly as the existing unknown-service refusal does, so
the attribute set on a refused request is unchanged. A refusal on this route
continues to emit the governance-audit event the scoped-route admission path
already emits -- `governanceaudit.EventTypeReadAuthorization` with
`governanceaudit.DecisionDenied`. As in commit 1 the reason code an operator
reads changes meaning rather than appearing: `scoped_route_not_enabled` before,
and after this change a grant-bearing caller is admitted while only an
all-scope browser session under a fail-closed `BrowserSessionRoutePolicy` is
refused, with the existing #6450 `scoped_route_all_scope_grant_required`.
