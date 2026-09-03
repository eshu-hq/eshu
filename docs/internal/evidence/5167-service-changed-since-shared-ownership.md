# One service id, two tenants: the service changed-since grant hole

`GET /api/v0/freshness/services/changed-since` binds the caller's grant by
resolving the catalog `service_id` through the service-catalog correlation
facts. That binding is in
[5167-freshness-family-allowlist.md](5167-freshness-family-allowlist.md); this
document covers the hole review round 3 found in it (PR #6472, Codex P1) and
the fix.

## Withdrawn from #6472

The route's promotion was pulled from PR #6472. The fence described in this
document ships and is tested, but it is not enough on its own to move
`GET /api/v0/freshness/services/changed-since` off `pendingRowFilteringRoutes`,
so the route keeps its ledger row and every scoped token and browser session
still gets a middleware 403. Ledger 24 -> 22, not 24 -> 21.

The reason is [the liveness gap](#the-liveness-gap) below. Both correlation
probes see a correlation only while it is live in its scope's active
generation, so a tenant whose correlation ages out stops contesting the
`service_id` while its lineage generation is still the active one -- and the
other tenant is admitted onto that lineage. The header of
`pendingRowFilteringRoutes` states the contract this violates: a route promoted
onto the scoped allowlist must never turn a caller's existing 403 into a
cross-tenant read. A fence that an aged-out correlation defeats does exactly
that, so it does not clear the bar for promotion.

Three findings settle that the lineage rows cannot be attributed to a tenant
without a schema change, so no in-handler rework closes this:

- `ownershipEvidencePayload` (`go/internal/reducer/service_materialization.go`)
  writes `owner_ref`, `provider`, `entity_ref`, `lifecycle`, and `tier` only.
  The evidence snapshots carry no repository, scope, or tenant.
- `service_materialization_generations` has no scope column at all
  (`schema/data-plane/postgres/025_service_materialization_generations.sql`),
  and `service_materialization_generations_active_service_idx` makes the active
  generation unique per `service_id` across every tenant.
- `source_intent_id` is nullable and the intent it points at is deleted by
  generation retention before the generation is, as
  [Why it is not closed here](#why-it-is-not-closed-here) works through.

#6475 -- the scope column on both lineage tables plus a `(scope, service_id)`
writer key -- is what makes the promotion possible, and the promotion itself is
then the ledger header's ordinary four-step move. Until it lands the fence is
defense in depth: inert on the live route, and already proven against the two
tenant shapes it will have to hold once callers reach it.

## The defect

Admission was existential. `serviceChangedSinceGrantAdmits`
(`go/internal/query/freshness_service_changed_since.go`) asked
`ListServiceCatalogCorrelations` for one row matching the requested
`service_id` under the caller's grant arrays, and admitted the lineage read as
soon as one came back.

Catalog service ids are not tenant-qualified. `serviceCatalogAdmittedServiceID`
(`go/internal/reducer/service_catalog_correlation_classify.go`) returns the
catalog entity ref verbatim, so a component named `api` in the `default`
namespace is `component:default/api` in every tenant that declares one.

Nothing downstream re-qualifies it:

- `schema/data-plane/postgres/025_service_materialization_generations.sql` and
  `026_service_evidence_snapshots.sql` carry `service_id` and generation
  columns only -- no repository, no scope, no tenant.
- `serviceMaterializationWriter`
  (`go/internal/reducer/service_materialization_writer.go`) conflicts on
  `service_id` alone, so two tenants materializing the same id overwrite one
  lineage rather than keeping two.
- `ComputeServiceChangedSinceDelta`
  (`go/internal/storage/postgres/service_changed_since_sql.go`) selects the
  active and prior generation for a `service_id` with no other predicate.

So when two tenants both ran a service called `api`, tenant A's own correlation
admitted the request, and the response -- current and prior generation ids and
timestamps, per-family added/updated/retired/superseded counts, and the bounded
`service_evidence_key` samples, which embed owner refs, deployment identities,
and incident identities -- came from whichever tenant materialized last.

The synthetic probe below shows the admission half of that directly: under
tenant A's grant, the shipped correlation query returns
`fact-shared-scope-alpha` for the contested id, the row that used to admit the
read.

## The fix

Admission is now exclusive: one correlation inside the grant **and** none
outside it. A contested id is refused with the route's ordinary
service-not-found and a new refusal reason, `shared_ownership`.

The boundary this buys is bounded: it reaches every repository with a
**currently active** catalog correlation for the service, and no further.
Commits seven through ten said exactly that in the caller-facing sentences, and
the withdrawal took those sentences back out along with the promotion, so the
shipped surfaces now describe the 403. See
[The liveness gap](#the-liveness-gap) for what the fence excludes and why it
cannot be closed without #6475.

Refusing is the honest answer here, not a placeholder for one. The lineage rows
carry nothing to filter on, so a scope-aware delta needs a scope column on
those two tables and a reducer writer keyed on `(scope, service_id)` -- a
schema and materialization change with its own migration and backfill,
deliberately out of scope for this PR and tracked as
[#6475](https://github.com/eshu-hq/eshu/issues/6475).

### The store contract

`ServiceCatalogCorrelationFilter`
(`go/internal/query/service_catalog_correlations.go`) gains one field,
`OutsideGrant`, which selects
`listServiceCatalogCorrelationsOutsideGrantQuery`: the same statement with a
stricter negation in place of the grant clause. The two are deliberately not
complements -- see
[5167-service-changed-since-ambiguous-candidates.md](5167-service-changed-since-ambiguous-candidates.md),
which is where that clause's current shape and proof live.

Three properties are deliberate:

- The ordinary statement's text is untouched, so every other caller of this
  store keeps its plan cache entry.
  `TestServiceCatalogCorrelationsOutsideGrantQueryDiffersOnlyInTheGrantClause`
  reconstructs the ordinary statement from the inverted one and fails if they
  differ anywhere but the grant clause, and
  `TestPostgresServiceCatalogCorrelationsSelectTheStatementByOutsideGrant` pins
  which filter shape sends which text.
- Every membership test is wrapped in `COALESCE(..., FALSE)`. A payload with no
  `repository_id`, or no `candidate_repository_ids` array, compares to NULL;
  `NOT NULL` is NULL, which would drop exactly the rows whose ownership cannot
  be read -- the ones the probe most needs to report.
- The inverted clause has no empty-arrays arm. The ordinary clause is
  permissive on two empty arrays, so negating it would match every row and
  refuse every service, which reads exactly like ordinary tenant isolation.
  `ListServiceCatalogCorrelations` rejects that filter with
  `errServiceCatalogOutsideGrantNeedsAGrant` before it reaches SQL, and
  `TestPostgresServiceCatalogCorrelationsRejectAnOutsideGrantReadWithNoGrant`
  pins that no statement is sent.

The handler runs the exclusivity probe only after the grant already covers the
service, so an ungranted caller still pays for one query rather than two. The
two probes are separate statements and a correlation written between them is
not seen; that window belongs to checking before the read at all -- a single
combined query would leave the same gap between itself and the lineage read --
and it is bounded by how long the reducer takes to publish a new catalog
generation.

## The liveness gap

Both correlation reads join `ingestion_scopes ON scope.active_generation_id =
fact.generation_id` and require `generation.status = 'active'`, so the
exclusivity probe sees only correlations that are still live in their scope's
active generation. A correlation that has aged out -- the component removed
from the other tenant's catalog, that scope deactivated, the tenant offboarded
-- is invisible to it.

Its lineage is not. Nothing prunes `service_materialization_generations`:
`GenerationRetentionResult.RowsPruned`
(`go/internal/storage/postgres/generation_retention.go`) covers `fact_records`,
the content tables, `shared_projection_intents`,
`shared_projection_unroutable_intents`, and `scope_generations`, and neither
service lineage table appears anywhere in that pass. So the aged-out tenant's
generation stays the globally active one for the id
(`service_materialization_generations_active_service_idx` is `UNIQUE
(service_id) WHERE status = 'active'`), the id stops looking contested, and the
remaining tenant is admitted onto it. This is durable, not a race.

### Why it is not closed here

`service_materialization_generations` carries `source_intent_id`, which looked
like a way to recover the writing scope without a schema change. It is not one,
for three independent reasons found by reading the writer and the retention
pass:

- **Nullable, and no foreign key.** The column is `TEXT NULL` (schema 025), and
  `PostgresServiceMaterializationWriter.insertGeneration`
  (`go/internal/reducer/service_materialization_writer.go`) binds it through
  `nullableString(write.IntentID)`, so an empty intent id is stored as NULL.
  Nothing constrains it to resolve.
- **The intent is deleted first.** `deleteSharedProjectionIntentsForGenerationsQuery`
  (`go/internal/storage/postgres/generation_retention_sql.go`) removes
  `shared_projection_intents` rows for every pruned generation, while the
  service generation it produced survives. The reference dangles precisely in
  the aged-out case that needs it.
- **The scope is not reliably on the intent either.**
  `shared_projection_intents.scope_id` is `NOT NULL DEFAULT ''` (schema 008),
  and the retention file's own comment records that it "can be '' on legacy
  rows" -- which is why that table deliberately carries no foreign keys.

So the writing scope is not recoverable from committed state today. #6475 --
the scope column on both lineage tables plus a `(scope, service_id)` writer key
-- remains the only real fix, and this change bounds its claim to what the
query actually enforces rather than asserting a boundary it does not have.

`TestServiceChangedSinceSharedServiceIDIsRefused` carries the case as a
characterization test: the fake mirrors the active-generation joins, an
out-of-grant correlation marked aged-out does not contest the id, and the
caller is admitted. If a future change makes the probe see aged-out
correlations, that test fails, and the admission sentence has to be widened
with it when the promotion brings that sentence back.

## Red, then green

`TestServiceChangedSinceSharedServiceIDIsRefused`
(`go/internal/query/freshness_service_changed_since_shared_ownership_test.go`)
is new. The two-tenant fixture in
`freshness_service_changed_since_grant_test.go` gains a second owner for one
id, `component:default/api`, and a single lineage for it -- the shape the
tables actually produce. The ownership fake mirrors the new contract the way it
already mirrored the old one: it applies whichever of the two shipped grant
arms `OutsideGrant` selects, and returns the shipped sentinel for a grantless
outside-grant read.

Written first, with the store contract in place but no exclusivity check in the
handler:

```
$ go test ./internal/query -run 'ServiceChangedSince|ServiceCatalogCorrelation' \
    -count=1 > /tmp/5167r2-red1.log 2>&1; echo "EXIT=$?"
EXIT=1
--- FAIL: TestServiceChangedSinceSharedServiceIDIsRefused
    --- FAIL: .../a_shared_service_id_is_refused_before_the_lineage_read
        status = 200, want 404; a service id claimed outside the grant must not
        resolve; body = {"data":{...,"current_active_generation_id":
        "gen-current-shared","service_id":"component:default/api",...}}
    --- FAIL: .../the_refusal_is_indistinguishable_from_an_absent_service
        shared: 200 {"data":{...}}   absent: 404 {"error":{"code":
        "service_not_found",...}}
--- FAIL: TestServiceChangedSinceGrantRefusalIsRecordedOnTheSpan
    --- FAIL: .../shared_service_id_records_shared_ownership
        span is missing eshu.service_changed_since.grant_refused
```

That red is the defect: tenant A's scoped token, 200, carrying the contested
lineage's active generation id. Green after the exclusivity probe landed:

```
$ go test ./internal/query -run 'ServiceChangedSince|ServiceCatalogCorrelation' \
    -count=1; echo "EXIT=$?"
ok  	github.com/eshu-hq/eshu/go/internal/query	1.221s
EXIT=0
```

## BITES

Each mutation was applied alone, run, and reverted. Exit codes captured
directly.

| Mutation | Case that fails | Exit |
| --- | --- | --- |
| Exclusivity check neutered (`return true` after the in-grant probe: admit on any granted row) | `a shared service id is refused before the lineage read`: 200 with `gen-current-shared`; `the refusal is indistinguishable from an absent service`: 200 against 404; `shared service id records shared_ownership`: no span attribute | 1 |
| Store ignores `OutsideGrant` and always sends the ordinary statement | `TestPostgresServiceCatalogCorrelationsSelectTheStatementByOutsideGrant/outside-grant_read`: the ordinary text is sent | 1 |
| `errServiceCatalogOutsideGrantNeedsAGrant` guard dropped | `TestPostgresServiceCatalogCorrelationsRejectAnOutsideGrantReadWithNoGrant`: the statement runs instead of erroring | 1 |
| Ownership fake ignores the active-generation joins | `an aged-out out-of-grant correlation no longer contests the id`: 404, because the fake now contests an id the shipped statements cannot see | 1 |
| Shipped code | pass | 0 |

The second row is the one the handler tests cannot see: they drive a fake, so a
store that quietly ran the un-negated statement would keep every handler
assertion green while production admitted every contested id.

The withdrawal has its own mutation. Letting `scopedFreshnessDeltaRoute` match
`/api/v0/freshness/services/changed-since` again -- the one-line change that
would re-promote the route -- is caught by
`TestScopedTokenReachesFreshnessDeltaPairOnly` and
`TestServiceChangedSinceStaysOnPendingLedger`
(`go/internal/query/auth_scoped_routes_freshness_delta_test.go`):

| Mutation | Case that fails | Exit |
| --- | --- | --- |
| `scopedFreshnessDeltaRoute` matches the service path again | `TestScopedTokenReachesFreshnessDeltaPairOnly/service_changed_since_still_pending`: 200, want 403; `TestServiceChangedSinceStaysOnPendingLedger`: `ScopedHTTPRouteSupportsTenantFilter() = true, want false while the route is pending` | 1 |
| Shipped code | pass | 0 |

`TestPendingRowFilteringRoutesDisjointFromScopedAndSharedKey` stays green under
that mutation, which is why the two tests above exist: the disjointness gate
compares ledger map keys, and a matcher that admits a route the maps still call
pending is invisible to it. The middleware 403 and the predicate are what pin
the withdrawal.

## Performance Evidence

`EXPLAIN (ANALYZE, BUFFERS)` on the candidate statement, run before the
production code was written, in the same throwaway environment the in-grant
probe was measured in: PostgreSQL 16 container on an Apple-silicon laptop
(non-comparable to any reference profile, and no absolute target is claimed),
data-plane schema applied from `schema/data-plane/postgres` in filename order
(53 files, 0 failures), synthetic rows only -- no real repository, service,
owner, or scope name. Bound through `PREPARE` + `EXPLAIN ... EXECUTE`,
mirroring the pgx `cache_statement` default the store runs under.

Data: two scopes (`scope-alpha`/`repo-aaaa`, `scope-beta`/`repo-bbbb`), one
active generation each, three single-owner correlations per scope, one
contested id (`svc-shared-0001`) correlated once from each scope, plus 10,000
bulk correlations split across the two scopes. 10,008 `fact_records` rows,
`ANALYZE` after the load.

| Shape | Rows | Plan | Execution | Buffers | Planning |
| --- | ---: | --- | ---: | --- | ---: |
| Contested id, tenant A's grant | 1 | Index Scan using `fact_records_service_catalog_correlations_service_idx` | 0.052 ms | shared hit=11 | 0.966 ms |
| Single-owner granted id, same grant | 0 | same index | 0.023 ms | shared hit=7 | 0.603 ms |
| Other tenant's own id, same grant | 1 | same index | 0.025 ms | shared hit=7 | 0.544 ms |

`Index Cond: ((payload ->> 'service_id') = ...) AND (generation_id =
scope.active_generation_id)`, with the negated grant disjuncts as the filter --
the same index and the same shape the in-grant probe takes. The exclusivity
check is therefore a second bounded index lookup on the scoped path, not a
scan: roughly a millisecond per scoped request, dominated by planning, flat in
corpus size.

Row truth, not only plans: the contested id returns `fact-shared-scope-beta`
(tenant B's row) under tenant A's grant, so the probe does see the second
owner; the single-owner id returns nothing. The shipped in-grant statement
returns `fact-shared-scope-alpha` for that same contested id, which is exactly
the row that used to admit the read.

One hazard, recorded rather than hidden, identical to the in-grant probe's:
under `force_generic_plan` the statement falls back to
`fact_records_collector_status_active_idx`, 5,004 rows removed by filter,
`shared hit=1559`, 4.844 ms execution. It does not fire under
`plan_cache_mode=auto`, which keeps the custom plan.

A grantless outside-grant read -- the shape the store refuses -- returns a row
for a service the caller owns, which confirms the fail-closed direction of the
negated clause and why that guard is fail-loud rather than permissive.

No-Regression Evidence: the ordinary statement's text, bind arguments, joins,
and index are unchanged, so its plan cache key is byte-identical to
`origin/main`; two tests pin that. The unscoped path still issues no ownership
query at all (`shared key never consults the ownership store`), and a scoped
caller with an empty grant still issues none (`empty grant touches neither
store`).

## Observability Evidence

`eshu.service_changed_since.grant_refused_reason` gains one value,
`shared_ownership`, declared in
`go/internal/telemetry/contract_zzzz_service_changed_since.go` next to
`empty_grant`, `not_granted`, and `ownership_unwired`. The vocabulary stays
closed and low-cardinality: it carries no service id, tenant, workspace,
repository, or scope, because the caller-facing body is deliberately
byte-identical to an unknown service's and the span is the only place an
operator can see the difference.

The reason matters operationally. `not_granted` is a boundary working as
intended; a run of `shared_ownership` means two tenants have collided on a
catalog id and neither can read that service's delta. That is the signal that
the schema change named above is now worth doing.

`TestServiceChangedSinceGrantRefusalIsRecordedOnTheSpan` asserts the attribute
and the reason on the shared case, and
`TestServiceChangedSinceGrantRefusalReasonsAreAClosedVocabulary` pins the
literal `shared_ownership`. No metric and no log key is added, so no
`docs/public/observability/telemetry-coverage.md` row is owed.

## Contract text

This section is history. Commits seven through ten put an admission sentence
on four surfaces, kept identical in all of them: the OpenAPI operation
description (`openAPIPathsFreshnessServiceChangedSince`,
`go/internal/query/openapi_paths_service_changed_since.go`), the
`get_service_changed_since` tool description
(`go/internal/mcp/freshness/tools.go`),
`docs/public/reference/http-api/status-admin.md`, and the tool's row in
`docs/public/reference/mcp-tool-contract-matrix.md`. It read: scoped tokens
receive a service only when every repository with a currently active catalog
correlation for it is in the grant; an ungranted `service_id`, or one also
correlated outside the grant, returns the same not-found an unknown one
returns; a correlation that has aged out of its scope's active generation no
longer contests the id (#6475). "Currently active" was load-bearing, not
hedging -- it named exactly the liveness the two statements enforce.

The withdrawal commit deleted that sentence from all four surfaces together
with the promotion. A route whose middleware refuses every scoped caller must
not advertise what it would do for one. What ships instead is the refusal, and
it names who is refused rather than saying "scoped tokens and browser
sessions", which over-claimed: `browserSessionRouteDenialReason` admits a
tenant-bound all-scope console session on any route outside the scoped-token
allowlist wherever `ScopedRoutePolicyForGovernanceMode` sets
`AllowTenantBoundAllScopes`, and this route is outside that allowlist.
`openAPIPathsFreshnessServiceChangedSince` now says scoped tokens are refused
"in every deployment, and so is every browser session except a tenant-bound
all-scope console session", and that `local_no_policy` and
`hosted_single_tenant` admit that console session "as it does on every route
outside the scoped-token allowlist". The `get_service_changed_since`
definition, the status-admin section, and the contract-matrix row say the same
in the space each has. The admission sentence comes back with the promotion,
once #6475 gives the lineage tables a column naming the tenant.

`TestToolsPreserveFreshnessRegistrationContract` pins a SHA-256 over the
marshalled freshness tool definitions. The admission wording moved that pin
from `197bfde6...` to `d1349562...` while it stood; the withdrawal and the
403 wording moved it to `ca92b326...`, and naming the admitted browser session
moved it again, to `74856c94...`. Neither
a cassette nor a B-12 snapshot entry carries tool or operation description
text, so nothing is regenerated.

## What a caller loses

Nobody loses it today. While the withdrawal stands the middleware answers
every scoped caller with a 403 before the handler runs, so what follows is what
the route will do for a caller once #6475 lets it be promoted, not what one
sees now.

A scoped caller that legitimately owns a service whose catalog id another
tenant also uses will then get not-found where it previously got a delta -- one
that may well have been the other tenant's. Losing an answer that could be
wrong is the right trade for a tenant boundary, and an unscoped operator still
reads the service. The `shared_ownership` reason is what tells an operator this
is happening, rather than leaving it to look like a missing service.

What the fence does **not** buy, and should not be read as buying: protection
from an aged-out competing correlation. That case, described in
[The liveness gap](#the-liveness-gap), still admits the read and still hands
over the other tenant's lineage. The caller-facing sentences bounded it while
the promotion stood; they now say the route refuses scoped tokens outright, and
the gap stays tracked as #6475. It narrows the hole rather than closing it,
which is why the promotion was pulled.
