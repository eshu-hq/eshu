# Freshness delta pair: off the pending ledger, onto the scoped allowlist

`GET /api/v0/freshness/changed-since` and `GET /api/v0/freshness/generations`
were the last two `freshness` entries in `pendingRowFilteringRoutes`
(`go/internal/query/auth_scoped_routes_pending_row_filtering.go`), the #5167
Group B ledger of MCP-reachable routes that bind no caller grant and therefore
answer a scoped or browser-session caller with a middleware 403. Both handlers
gained real grant filtering earlier in F-6. This change performs steps 2-4 of
the ledger header's own removal procedure for them: a matcher, the ledger move,
and the OpenAPI marker. No handler or SQL logic is touched.

## What moved

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

## Why it is safe

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

## Tests run

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

## BITES: the allowlist wiring

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
