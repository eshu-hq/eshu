# #5167 F-6 W6: scoped-token grant filtering for cloud/aws/kubernetes/observability/ecosystem/relationships routes — performance & observability evidence

W6 promotes 11 MCP-reachable routes off the `pendingRowFilteringRoutes` ledger
onto the scoped-token allowlist, binding each read to the caller's
`AllowedRepositoryIDs`/`AllowedScopeIDs` grant. Three of the touched files carry
hot-path Cypher (`relationships_catalog_cypher.go`, `relationships_catalog.go`,
`infra_ecosystem_overview.go`), so this note records why the change is within the
repo-scale performance contract and carries no observability change.

The controlling fact for every route below: on all of these routes, **every
caller that existed before #5167 was either a shared-key/admin caller (which
this change leaves byte-identical) or a scoped/browser-session caller (which
previously received a transport-layer 403 before the handler ran).** There is no
pre-existing scoped traffic on these routes to regress; the scoped path is new
capability, not a rewrite of a served path.

## Backend

Pinned NornicDB per `docker-compose.yml` / `deploy/helm/eshu`. The scoped
relationships/edges query shape is validated by the `query-plan-regression` gate
(`scripts/verify-query-plan-regression.sh`, which drives a live Bolt `PROFILE`)
and the `go test ./internal/queryplan` source-hash lockstep.
`go/internal/queryplan/testdata/hot-cypher.yaml` `QP-RELATIONSHIPS-EDGES`
`source_sha256` tracks the current `relationshipEdgesCypher` source — including
the P1 review fix that swapped `infraResourceScopePredicate` for
`relationshipEndpointScopePredicate` (dropping the DEFINES disjunct), so the
digest was refreshed to that post-fix source. The queryplan source-hash gate
confirms the tracked fixture matches the shipped query; the live-backend
`PROFILE` regression check runs in this branch's `make pre-pr` promotion gate and
in CI's `query-plan-regression` job against this head.

## No-Regression Evidence: unscoped/admin hot path is byte-identical

The scope predicate renders **only** for a scoped caller; the unscoped/admin
query is emitted unchanged.

- `relationshipEdgesScopeWhereClause(entry, access)` returns `""` when
  `!access.scoped()`, so `relationshipEdgesCypher`/`relationshipEdgesCypherFiltered`
  emit the exact pre-#5167 string for a shared/admin caller — no WHERE clause,
  same MATCH anchor, same indexed source-anchor ordering, same `LIMIT`.
- `runEcosystemOverviewCounts` uses each entry's original `entry.cypher`
  (unchanged single-label count) for a non-scoped caller; the grant-bound
  `entry.scopedCypher` variant renders only when `access.scoped()`.

This is asserted by per-route no-regression tests that dispatch a shared/admin
request and fail if any access-scoping predicate appears in the emitted query:
`TestGetRelationshipEdgesUnscopedQueryStaysUnfiltered`,
`TestKubernetesListCorrelationsUnscopedQueryStaysUnfiltered`,
`TestObservabilityCoverageListCorrelationsUnscopedQueryStaysUnfiltered`,
`TestCloudInventoryHandlerUnscopedQueryStaysUnfiltered`,
`TestListRepositoriesByLanguageUnscopedQueriesStayUnfiltered`, and
`TestScopedIaCResourceListUnscopedQueryUnchanged`. Because the emitted query is
character-for-character identical, the unscoped path's plan, db-hits, and
latency are unchanged by construction — before == after with no measurement gap.

## Benchmark Evidence: scoped path is bounded and plan-validated

The scoped relationships/edges query keeps the same MATCH anchor and the same
`LIMIT $limit` page bound as the unscoped query; the added predicate is a
post-MATCH filter on indexed identity properties
(`s.repo_id`/`s.id`/`t.repo_id`/`t.id` `IN $allowed_*`) plus a bounded `EXISTS`
Workload-fallback subquery reusing the already-in-production
`infraResourceScopePredicate` shape. The `query-plan-regression` live `PROFILE`
gate (above) confirms the scoped query keeps its indexed traversal seed and does
not fall back to an outer/merge full-store scan.

The scoped ecosystem-overview counts are `label_inventory` reads with
`max_results: 1` (declared in
`go/internal/queryplan/testdata/query-source-coverage.yaml` for
`runEcosystemOverviewCounts`), and the empty-grant caller short-circuits to
all-zero counts with **no** graph read at all — the `#5137` `LiveActivityStore`
zero-grant precedent. The Postgres array-bind routes (cloud inventory,
kubernetes/observability correlations, repositories by-language/inventory) add a
`repo_id = ANY(...)`/`scope_id = ANY(...)` predicate to an already-bounded,
already-`LIMIT`ed reducer-fact read, and likewise skip the query entirely on an
empty grant. The two drift routes (aws/cloud runtime-drift) add a caller-side
grant precheck that returns the zero-finding page **without** calling the store
for an empty/out-of-grant caller (proven by
`TestHandleAWSRuntimeDriftFindingsScopedAccountOnlyNeverCallsStore` and
`TestHandleCloudRuntimeDriftFindingsScopedOutOfGrantNeverCallsStore`), so the
scoped path is never more expensive than the unscoped one and is cheaper on an
empty grant.

## No-Observability-Change: pure query construction, no new telemetry surface

No-Observability-Change: no runtime metric, span, log field, queue stage, worker/lease/batch knob, or
Compose/Helm runtime setting is added or changed. The scoping is pure query
construction (a conditionally-rendered WHERE clause / SQL predicate) plus
caller-side grant prechecks; each handler keeps its existing query-handler span
and truth envelope. The ecosystem-overview and graph-summary truth envelopes
downgrade to `TruthLevelDerived` for a scoped caller (the counts are then
grant-bound rather than whole-corpus), which is a response-truth label carried
in the existing envelope, not a new telemetry surface. An operator diagnoses a
scoped caller's empty/filtered result the same way as any grant-bounded read:
the request's existing handler span and the caller's `AuthContext` grant.
