# #5385 workload identity re-key flip: performance and observability note

Flip wave (second commit group): `go/internal/workloadid` constructors emit
the repository-scoped key (`workload:<repo_id>:<name>`,
`workload-instance:<repo_id>:<name>:<environment>`); every production caller
already passed its repository through the constructor (projection,
projection_helpers RUNS_ON row, DEPENDS_ON target, catalog identity row,
identity-only context row), so the flip lands inside the constructors plus
test/fixture/golden pins. No executable query text changes in this wave.

## No-Regression Evidence:

One new Cypher read lands in this wave (T1): the /catalog SAME_NAME
sibling enrichment (`QP-DEPLOY-CATALOG-WORKLOAD-SIBLINGS`), a single
workload-anchored connected path bounded to the base query's `$ids` set
under the issue-#3389 discipline (workload-anchored, `WHERE w.id IN $ids`,
`ORDER BY` present, no `LIMIT` to satisfy). Sibling admission is decided
Go-side against the caller grant (Decision D2), keeping the query
grant-free and backend-portable; the /catalog row filter it feeds is pure
Go over already-read rows. No index, transaction scope, batch size, worker
count, or queue/lease path changes. Every other statement keeps its text:
the values bound to `$workload_id` / `$instance_id` parameters change shape
(scoped instead of name-only) while anchors (`Workload {id}`,
`WorkloadInstance {id}`, `WorkloadInstance {workload_id}`) and
plan-relevant predicates stay byte-identical, so planner choices cannot move
on account of the re-key itself.
Proof:

- `go test ./internal/queryplan/ -count=1` green: the `QP-SVC-RUNTIME-TOPOLOGY`
  pin (anchor `WorkloadInstance.workload_id`, `NodeIndexSeek`, no
  `AllNodesScan`/`CartesianProduct`) still matches the unchanged
  `FetchWorkloadRuntimeTopology` source text; the new
  `QP-DEPLOY-CATALOG-WORKLOAD-SIBLINGS` entry is registered with the same
  operator expectations, and its live `PROFILE` (NodeIndexSeek-anchored,
  no AllNodesScan) is measured in the T1 live proof section below.
- `go test ./internal/reducer/ ./internal/query/... ./internal/ifa/...`
  green on the flipped tree: projection, ladder resolution, and
  materialization behave identically apart from the id values asserted.
- Full live B-7 golden-corpus gate on the pinned Neo4j
  (`ESHU_GRAPH_BACKEND=neo4j bash scripts/verify-golden-corpus-gate.sh`)
  green with the regenerated B-12 snapshot: end-to-end replay (collect,
  reduce, project, query) converges with unchanged node/edge family counts
  (the corpus has no same-name collisions) and terminal drain counts equal
  to the pre-flip run. Backend: pinned Neo4j per
  `docker-compose.neo4j.yml`; input shape: the 20-repo B-12 corpus plus
  B-10 cassettes; the gate's own `N pass, 0 required-fail` terminal line is
  the row-count evidence.
- Constructor cost is pure string formatting (one `Sprintf` over three
  trimmed segments, no I/O, no allocation beyond the id itself); no new
  graph read, index seek, or batch is added by this wave. The one
  `workload_name` index seek per handle already exists from the ladder wave
  and is unchanged here.
- Live-drain root-cause fix in this wave: `Poll`-based instance wait
  (`go/internal/reducer/workloadinstance/wait.go`) could decline the cheap
  path on a ledger-frozen handle-form anchor (`workload:<name>`), whose
  re-read can never converge to the scoped materialization key after the
  flip. `Poll` now falls back to full evaluation (with workload
  re-resolution) on any error or non-ready state from the cheap path,
  instead of returning `ErrNotReady`. Regression:
  `TestWaitPollFallsBackOnHandleFormAnchors` plus cloud-side
  `TestWorkloadCloudRelationshipCommitsBeforeWaitingOnInstances` green. Live
  B-7 first run (pre-fix binary) reproduced the symptom exactly: drain stuck
  at `fact residual=2` on `workload_cloud_relationship_materialization`
  (`2 workload instance anchor(s) not materialized`); the gate re-run lints
  this wave's fix as the remedy.

Why this is safe: identity VALUES move but no lookup PATH does. Exact-match
reads (`{id: $x}`) are format-agnostic while both sides move together, and
the retirement cascade converts every construction site (reducer projection,
RUNS_ON row, DEPENDS_ON target, catalog and context synthesis) through the
same constructors, so no side keeps the old format. Handle-space readers
(`workload:<name>` prefix tests, strippers, the ladder) keep working because
the public handle format does not move.

## Read-Side Retirement (live B-7 found, fixed in this wave):

The first full live B-7 run past the drain fix failed 7 required checks,
all one shape: callers pass the public handle (`workload:api-svc`,
`workload:deployable-source`, per the gate pin tables, which keep handle
arguments and expect scoped values) into read paths that matched
`w.id = $x` with the raw input. Pre-flip handle == id so id-only lookups
worked; post-flip they 404. The MCP layer forwards `workload:` selectors
whole precisely so the API ladder resolves them server-side
(`normalizeQualifiedIdentifier`), so these four server paths grew rung 2:

- `entity` workload context/story (`workloadContextLookupClause`): handle
  reads the bounded name-candidate rung with the stripped name through the
  existing #6786 grant-admission machinery; scoped ids keep the exact rung,
  bare inputs keep the id rung (still 404, no contract widens).
- `entity` service context (`fetchServiceWorkloadContext`): the name-first
  probe strips; the id rung keeps the raw input (scoped ids and legacy
  handle-shaped nodes still match exactly).
- `entity` service story/intelligence-report (`collectServiceWorkloadCandidates`
  service_id branch): a handle service_id falls back to the stripped-name
  probe after the id rung misses.
- `compare` (`fetchWorkload`/`fetchWorkloadByName`, new
  `compare/workload_lookup.go` after the 500-line cap forced the split):
  handle resolves the stripped name through a bound+1 candidate read with
  lowest-granted-id-wins; overflow, no-candidate, and no-granted-candidate
  read as missing, identically to an ungranted id hit.

Each fix landed RED-first (handle in, scoped row out) with the pre-existing
telemetry contracts intact (grant_denied counting, anchor-mismatch guards,
ambiguity errors). Six existing fixtures that pinned the retired
handle==id echo moved to the name rung with scoped expectations; their
asserted intent (authz-before-return, W3 leak filtering, denial counting)
is unchanged. `TestHotCypherManifestCoversEveryProductionQueryCall`
re-registered the touched symbols (fetchWorkload sha refresh,
fetchWorkloadByName as keyed_support single_key max_results 51, same class
as entity's `readWorkloadCandidates`).

Tail echo: `GetWorkloadStory` echoed the raw path param as `workload_id`,
re-emitting handle==id for every handle request that now resolves. It
echoes the resolved node id (`ctx["id"]`, raw param only as an impossible
fallback); RED test `TestGetWorkloadStoryEchoesResolvedScopedID` reproduces
the gate's `observed [workload:api-svc]` exactly. `GetWorkloadContext`
returns ctx directly (already scoped); its `context_path` link keeps the
typed handle, which still resolves through the ladder.

## T1 Live Proof:

Route admission (#5150 near-miss, caught by the live proof, not by unit
tests): the T1 handler filter shipped without a
`scopedHTTPRouteSupportsTenantFilter` matcher, an
`x-scoped-token-support` marker, or a `scopedTokenAdvertisedRoutes` entry,
so every scoped /catalog call 403d with "scoped authorization is not yet
enabled for this route" before the handler ran -- the exact recurrence the
ledger's own doc comment warns about. Fixed in this wave with TDD:
marker-only RED (`TestScopedTokenAllowlistCompleteness` failed naming both
missing pieces), then matcher (`GET /api/v0/catalog`, grant-bound class;
all-scope callers stay denied, so no `scopedRouteNeedsNoCallerGrant`
entry) plus ledger entry GREEN, plus
`TestScopedTokenAdvertisedRoutesReachHandlerThroughRealAuthMiddleware/GET_/api/v0/catalog`
PASS through the real middleware.

Functional live proof (`/tmp/t1-5385-proof.sh`, throwaway, built binary on
pinned Neo4j, alpha/beta repos sharing workload name `checkout` with
bidirectional SAME_NAME edges): alpha token sees exactly 1 workload row,
`same_name_siblings=0`, no `repository:r_beta` in /catalog, entity-map, or
change-surface bodies -- all `T1-PROOF PASS`.

Plan evidence: cypher-shell renders PROFILE as a compact summary (no
operator rows), so the operator tree was captured through the Bolt driver
against the pinned Neo4j with the production `:Workload(id)` lookup index
present (throwaway probe, deleted after measurement):

- `ProduceResults -> Projection -> PartialSort -> Projection -> Filter ->`
  `Expand(All) -> Filter -> Expand(All) -> NodeIndexSeek`, rows=2
  (bidirectional seed pair; Go dedupes siblings by repo), DbHits=25.
- `NodeIndexSeek` present, no `AllNodesScan`, no `CartesianProduct`.

Without the production index the same query plans
`DirectedRelationshipTypeScan` (measured before the index was added), which
is why the probe seeds the production DDL first: an index-less container
cannot prove the deployed shape.

## No-Observability-Change:

This wave adds no metric, span, log, or status signal. Runtime behavior
stays covered by the ladder wave's signals
(`eshu_dp_workload_identity_collision_total{mode}`,
`eshu_dp_workload_handle_resolution_total{surface,outcome}`), which are the
tripwires for a leaked name-only key (any post-flip `cross_scope_merge`
collision) and for handle-resolution pressure. Known gap, not introduced
here: `eshu_dp_workload_identity_rejected_total{reason}` from migration doc
section 8 does not exist yet, so constructor fail-closed drops (blank or
colon-bearing segments) are counted nowhere; filed as follow-up on #5385.
