# index-status: scoped callers get a grant-bound repository count

`GET /api/v0/index-status` and `GET /api/v0/status/index` (one shared handler)
were the first entry in `pendingRowFilteringRoutes`
(`go/internal/query/auth_scoped_routes_pending_row_filtering.go`), the #5167
Group B ledger. This change promotes both routes off it, class
`scopedRouteGrantBound`, following the #5137 withhold shape from
`status_operations.go`.

## What a scoped caller gets

Exactly this body, and nothing else:

```json
{
  "version": "...",
  "scoped": true,
  "repository_count": 1,
  "completeness_state": "scoped_repository_count_only",
  "withheld_sections": ["status", "reasons", "queue", "queue_blockages",
    "coordinator", "scope_activity", "aws_materialization",
    "semantic_extraction", "terraform_state"]
}
```

`withheld_sections` is every key the unscoped payload carries beyond `version`
and `repository_count`. `TestServeIndexStatusScopedBodyWithholdsDeploymentWideSections`
derives the expected set from a real unscoped response, so a section added to
`getIndexStatus` without being listed fails the build.

The decision was to withhold, not to keep aggregates and redact identifiers the
way `scopedCoordinatorToMap` does. A `queue_blockages` row reports `conflict_key`
as `COALESCE(conflict_key, scope_id)`, a raw scope id, and the stack-wide queue,
coordinator, and scope-activity counts are cross-tenant information on their own.
The status snapshot is not read for a scoped caller at all.

`repository_count` is bound in Cypher:

```cypher
MATCH (r:Repository) WHERE (r.id IN $allowed_repository_ids OR r.id IN $allowed_scope_ids) RETURN count(r) AS count
```

built from `RepositoryAccessFilter.GraphWhereClause` and `GraphParams`, the
binding the repository list count already uses. An empty grant answers 0 without
a graph call. A failed count is a 500 and an unconfigured graph a 503; zero is a
valid, materially different answer for a scoped caller, so it is never the
fallback. Shared-key callers reach `getIndexStatus` unchanged. An all-scope
bearer token is now admitted wherever `ESHU_GOVERNANCE_MODE` admits a
tenant-bound all-scope caller on grant-bound routes (unset, `local_no_policy`,
`hosted_single_tenant`) and reads the full deployment-wide report there; before
this change it got a 403 in every mode. A tenant-bound all-scope console session
was already admitted to that report in the same modes and is unchanged.
`hosted_multi_tenant` and an unrecognized mode refuse both.

## The five promotion steps

1. Matcher: `scopedIndexStatusRoute` (`auth_scoped_routes_status.go`), wired into
   `scopedHTTPRouteSupportsTenantFilter`.
2. Both routes in `scopedTokenAdvertisedRoutes` as `scopedRouteGrantBound`.
3. `x-scoped-token-support` on both OpenAPI operations.
4. `403` (`#/components/responses/Forbidden`) kept on both operations, which
   `TestPolicyGatedRoutesDeclareForbiddenResponse` requires for a grant-bound
   class.
5. Removed from `pendingRowFilteringRoutes`; the ledger comment now records the
   promotion and the withhold shape.

`serveIndexStatus` dispatches: a scoped caller goes to `getScopedIndexStatus`,
everyone else to `getIndexStatus`. `getIndexStatus` is a
`grandfatheredNonHotSourceDigests` symbol, and editing it would force converting
its inventory entry; leaving it byte-identical keeps the unscoped path provably
unchanged. The new query callsite `getScopedIndexStatus` is registered in
`internal/queryplan/testdata/query-source-coverage.yaml` as typed `non_hot`,
`label_inventory`, label `Repository`, `max_results: 1` (the same disposition as
`queryRepositoryTotal`), with the digest the production-source discoverer
reports.

The MCP tool `get_index_status` description drops the 403 disclosure for the
scoped shape, and its plain-payload summary now renders the scoped shape instead
of falling back to the generic summary. `eshu mcp setup --verify` smokes this
route with a personal token; `TestAPIQueryProberAcceptsScopedIndexStatusShape`
pins that the smoke passes on the scoped body.

## Proof

RED, before the change: a scoped caller of either route gets
`403 permission_denied` ("scoped authorization is not yet enabled for this
route"), in both the handler tests and the live test.

RED, allowlist only (matcher, class, ledger move, no handler change): the
handler serves a scoped caller the full deployment-wide report. The repository
count read 2 with one granted repository, and the body carried the other
tenant's raw scope id from `queue_blockages.conflict_key`, its coordinator
instance id, and its display name. This is the leak the withhold shape closes.

GREEN: `TestServeIndexStatusScoped*` and `TestServeIndexStatusSharedCallerPayloadUnchanged`
in `go/internal/query/status_index_scoped_test.go`:

- N1: repository_count 1 with a two-repository graph, and the fake graph saw a
  statement containing `IN $allowed_repository_ids` with params holding only
  repo-a. The fake evaluates membership from the params it received, so the
  count of 1 proves the grant is in the query and is not a post-filter.
- N2: empty grant, count 0, zero graph calls.
- N3: exact key set, exact `withheld_sections`, and a substring search of the
  serialized body finds neither repo-b's id nor the raw scope id seeded as a
  `queue_blockages.conflict_key`. The fixture asserts the unscoped body does
  carry that scope id, so the search is not inert.
- N4: shared-key payload keeps every section and gains no scoped key; the
  `eshu index-status` CLI golden is untouched.
- N5: `TestScopedTokenAdvertisedRoutesReachHandlerThroughRealAuthMiddleware`
  and `TestPolicyGatedRoutesDeclareForbiddenResponse` pass.

Live, against an isolated NornicDB (the image pinned in `docker-compose.yaml`,
container `nornic-5167idx`, host bolt port 17997, removed afterwards):
`TestIndexStatusScopedRepositoryCountNornicDBLive` seeds three Repository nodes
and reads the count back through the real handler and `Neo4jReader`. This build
silently ignores an invalid `WHERE` and returns every row, so only a
row-membership test proves the filter. Cases: one granted repository 1, two
granted 2, a scope grant naming a repository 1, repository grant plus scope grant
2, the same id in both lists 1 (no double count), a grant naming no repository 0,
and an empty grant 0 with zero graph calls.

Performance Evidence: measured once on the pinned NornicDB image
(ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-500-e022384c, amd64 container on an
Apple Silicon Docker host, Bolt from the host, so driver round trip included) with
2002 `:Repository` nodes carrying only `id` and `name`; the fixture defines no
index on `Repository.id`, so the statement is a label scan plus a list membership
filter, the same shape as the unscoped baseline. Median of 30 executions each,
with a unique `$nonce` param per execution because this build caches read results
by (statement text, params). The unscoped
`MATCH (r:Repository) RETURN count(r) as count` baseline was 6.36 ms and returned
2002. The grant-bound statement was 6.77 ms at a grant of 1 repository (count 1),
6.84 ms at 8 (count 8), and 6.66 ms at 128 (count 128). The scoped read costs
about 0.3 to 0.5 ms over the unscoped count and is flat in grant size. An earlier
probe on the same build recorded 3.3 to 4.2 ms at those grant sizes; the two
figures differ in harness, not in shape. A scoped call runs one graph read and
skips the Postgres status snapshot the unscoped report loads. The read returns one
`count(r)` row, so ordering and limit do not apply.

No-Observability-Change: the read runs behind the existing API request
instrumentation, so `eshu_dp_api_request_duration_seconds` and
`eshu_dp_api_request_errors_total` (label `route`) cover both routes for a scoped
caller, and a failed count surfaces as a 500 or 503 there rather than as a silent
zero. No new span, metric, log key, or knob is added.
