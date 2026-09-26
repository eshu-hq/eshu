# #6475 part B: service changed-since reads bind the grant to the lineage scope

Scope of this note: the read half of #6475. Part A
([6475-service-lineage-scope.md](6475-service-lineage-scope.md)) put the
writing ingestion scope on every `service_materialization_generations` row.
Part B binds the caller's grant to that column in the resolve SQL, adds an
explicit `scope_id` selector, answers an ambiguous service id with 409, scopes
the prior-generation lookup to the resolved lineage, retires the
`shared_ownership` correlation fence, and promotes
`GET /api/v0/freshness/services/changed-since` off `pendingRowFilteringRoutes`.

## Defect

Root-Cause Evidence: `resolveServiceChangedSinceScopeQuery` took `$1
service_id` only and picked one active generation with `LIMIT 1`, and
`resolveServiceChangedSincePriorGenerationQuery` matched `(service_id,
generation_id)` only. Once part A let one service id hold one lineage per
ingestion scope, a scoped caller could be served another scope's lineage, and
could diff against another scope's prior generation. The live regression
`TestServiceChangedSinceBindsGrantToLineageScopeLive` failed on the part A head
(`c1bc1e7e1`) with, for tenant A's scoped filter
(`AllowedScopeIDs=[scope-a]`):

```text
summary = {ServiceID:component:default/api ScopeID: ... SinceGenerationID:gen-a-prior
  CurrentActiveGenerationID:gen-b-current ...}; want scope-a lineage gen-a-prior -> gen-a-current
```

That is tenant A reading tenant B's current generation. All eleven subtests
failed; the run exited 1.

## Contract

- Grant binding, in SQL on the lineage row:
  `($3::boolean = false OR g.scope_id = ANY($5) OR (scope.scope_kind =
  'repository' AND scope.source_key = ANY($4)))`. A NULL `scope_id` matches
  neither arm, so an unattributed legacy lineage is invisible to every scoped
  caller.
- `scope_id` selector: `($2 = '' OR g.scope_id = $2)`, inside the same grant
  predicate, so an ungranted selector resolves nothing.
- Choice: one admitted attributed lineage is served; more than one, with no
  selector, returns `AmbiguousScopeIDs` (sorted, at most
  `MaxServiceScopeCandidates` = 20, plus a truncated flag) and no diff. The
  handler answers 409 with error code `ambiguous`.
- Unattributed lineage: served only to an unscoped caller and only when no
  attributed lineage exists. Part A's writer never supersedes a NULL-scope
  active row, so beside an attributed lineage it is stale by construction.
  Listing it in the ambiguity answer would make an admin read 409 for every
  service whose legacy row outlived its backfill witness. This refines the
  design's "unscoped operators also see unattributed" line and was reported
  to the coordinator.
- Prior generation: `AND scope_id IS NOT DISTINCT FROM $3::text`, bound to the
  resolved lineage (NULL for the unattributed one). A foreign prior id takes
  the existing not-found branch, and the response is byte-identical to an
  unknown id once the echoed selector is normalized.

## Proof

Live Postgres (`ESHU_POSTGRES_TEST_DSN`, local Postgres 18.6 container):

- `TestServiceChangedSinceBindsGrantToLineageScopeLive`: RED on `c1bc1e7e1`
  (all eleven subtests), GREEN after the change. It covers tenant-A-only
  resolution, foreign prior equals unknown prior, legacy invisible to scoped
  and visible to unscoped, both-scopes grant yields `[scope-a scope-b]`,
  unscoped caller never gets the legacy row beside attributed ones, selector
  inside and outside the grant, the repository grant through `source_key`, the
  empty grant, and a selector-bound prior lookup.
- `TestServiceChangedSinceResolvePicksAttributedNewestActiveLive` (part A's
  pick test) now pins the part B rule: two attributed lineages are reported as
  ambiguous in both insert orders, and the legacy row loses to an attributed
  one.

Handler (`internal/query/freshness`,
`TestServiceChangedSinceTwoTenantLineageBoundary`, over the grant-mirroring
`testutil.GrantMirroringServiceChangedSince`): RED on the part A handler
(the retired correlation probes still ran, the 409 did not exist, and the
legacy read carried no `unattributed` field). A mutation that deletes
`filter.Scoped = access.Scoped()` from the new handler turns
`explicit selector outside the grant` into `200` and the legacy lineage into
`200` for a scoped token, so the grant binding is load-bearing.

Middleware (`internal/query`, `TestServiceChangedSinceBearerTwoTenantBoundary`):
a restricted bearer is admitted and bounded under `hosted_multi_tenant` and
`local_no_policy`; an all-scope bearer is refused before the reader runs under
`hosted_multi_tenant`.

## Query plans

Seed (laptop-local, Postgres 18.6 in Docker on Apple Silicon, not the remote
test host): 8 repository scopes plus 20,000 filler `ingestion_scopes` rows;
5,000 services, each in one or two scopes, 12 generations per
`(scope, service)` lineage plus an unattributed legacy active on every tenth
service: 80,492 `service_materialization_generations` rows. Indexes as shipped
(`service_idx`, `observed_idx`, the `(scope_id, service_id)` partial unique
index). Target: `component:default/svc-3000` (two scopes plus a legacy row,
25 rows).

No-Regression Evidence: `EXPLAIN (ANALYZE, BUFFERS)`, 15 executions of each
prepared statement in one session on the same seeded state. Before (the
part A `LIMIT 1` resolve): median 0.047 ms, max 1.002 ms, top-level buffers
12 to 23 pages. After, unscoped: median 0.084 ms, max 0.224 ms, 16 to 19
pages. After, scoped by scope grant: median 0.072 ms, max 0.211 ms, 19
pages. After, scoped by repository grant: median 0.083 ms, max 0.291 ms, 19
pages. Both statements read the service's rows through
`service_materialization_generations_observed_idx` (service_id leading); the
new one joins `ingestion_scopes` through `ingestion_scopes_pkey` (Merge Right
Join on the 20k-row table, Hash Left Join on the 8-row one) and materializes
the per-service CTE once. The resolve is about 0.04 ms slower at the median and
stays under a third of a millisecond in every sample. It runs once per HTTP
request, so no new index is justified. The scope-bound prior lookup stays a
primary-key probe: 0.018 ms, 4 buffers, the same plan as before plus a heap
filter. An ambiguous answer now runs no count or sample statement; a scoped
not-found adds one `EXISTS` probe on `service_id` for telemetry.

## Telemetry

Observability Evidence: `eshu.service_changed_since.grant_refused_reason`
keeps `empty_grant` and `not_granted`; `not_granted` now comes from the
lineage read (`ServiceSummary.OutsideGrant`: rows exist for the id, none
admitted). `shared_ownership` and `ownership_unwired` are removed because
those refusals no longer exist. Two span attributes are new:
`eshu.service_changed_since.unattributed` (a served legacy read) and
`eshu.service_changed_since.ambiguous_scope_count` (the size of a 409 answer,
never the scope ids). `TestServiceChangedSinceGrantRefusalIsRecordedOnTheSpan`
and `TestServiceChangedSinceLineageAttributesAreRecordedOnTheSpan` pin them.

## Ownership store dependency removed

The route no longer reads the service-catalog correlation store, so the
nil-`ServiceOwnership` refusal was a refusal on a dependency nobody called: a
deployment without that store wired turned every scoped caller away from its
own lineage. `TestServiceChangedSinceServesScopedCallerWithoutOwnershipStore`
failed on `fc6667b99` (rc=1, tenant A got `404 service_not_found` for its own
`scope-a` lineage) and passes once the refusal, `Handler.ServiceOwnership`,
its `cmd/api` and `cmd/mcp-server` wiring, and the `ownership_unwired` reason
(emitted nowhere else) are removed.
