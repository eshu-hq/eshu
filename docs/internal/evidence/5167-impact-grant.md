# #5167 Impact Path Routes — Per-Node Ownership Instead Of A Cypher Grant

`POST /api/v0/impact/trace-resource-to-code`,
`POST /api/v0/impact/explain-dependency-path`, and
`POST /api/v0/impact/trace-exposure-path` leave `pendingRowFilteringRoutes`
(`go/internal/query/auth_scoped_routes_pending_row_filtering.go`) and join the
scoped-token allowlist (`scopedImpactCompareRoute`, class `scopedRouteGrantBound`).
All five ledger steps land together: the matcher, the advertised class, the
`x-scoped-token-support` marker, the retained `403` on each OpenAPI operation,
and removal from the ledger.

These walks cross nodes that carry no `repo_id` (CloudResource,
TerraformStateResource, Platform, CidrBlock, ...), so no single Cypher grant
predicate can bind them. The design (arbiter comment on #5167) is to fail
closed and check every node of the bounded page, per class, in the new
`go/internal/query/impact/ownership` package.

## What A Scoped Caller Gets

| Class | Owned when |
| --- | --- |
| `Repository` | its `id` is granted (Go) |
| `Workload`, `TerraformResource`, `TerraformModule`, `KubernetesWorkload`, `Function`, `SqlTable`, `ShellCommand` | its `repo_id` is granted (Go) |
| `WorkloadInstance` | its `repo_id` is granted (Go), or it has a `DEPLOYMENT_SOURCE` edge to a granted `Repository` (statement) |
| `CloudResource` | a `WorkloadInstance` whose `repo_id` is granted `USES` it (statement) |
| `TerraformStateResource` | a `TerraformResource` whose `repo_id` is granted `MATCHES_STATE` it (statement) |
| anything else | never |

- A path is dropped whole when any node on it is not owned.
- An ungranted anchor (start, source, or explain endpoint) is turned into the
  same `nil` an unknown anchor resolves to. The response is byte-identical to
  the unknown-anchor one and no traversal runs. `ResolveAnchor` resolves by id
  or name across 14 labels; without this rule it would be an existence oracle.
- A scoped caller's anchor resolves to every candidate carrying the id or name
  (`deployment.ResolveImpactAnchorCandidates`: `ORDER BY id, label` on the
  RETURN clause, `LIMIT 32`, then sorted and deduplicated in Go). One ownership
  `Check` judges them, and the first owned candidate is the anchor. A name two
  tenants share can no longer resolve to the foreign node and hide the
  caller's own node. The unscoped resolve keeps its `LIMIT 1`.
- An empty grant makes no graph call.
- `truncated` (or `coverage.truncated`) is computed from the raw row count
  before the Go filter, following the #6548 `...RowsInGrant` precedent.
- trace-resource-to-code binds the terminal Repository in Cypher before
  `LIMIT` with the P1 shape (`deployment.ImpactScopedRepoPathCypher`), then
  checks the interior in Go over `nodes(path)`.
- explain-dependency-path checks both endpoints before `shortestPath`. If any
  node on the returned path is not owned, the response is indistinguishable
  from no path.
- trace-exposure-path resolves the source through the grant: a foreign
  `source_entity_id` renders as not found. Name resolution was already bound
  to the grant in `ResolveExactGraphEntityCandidates`. Every chain node and
  sink is judged; `SecretsIAMSecretMetadataPath` and `CidrBlock` sinks are
  always withheld and named in `coverage.unresolved_reason`.
- Every scoped response carries `scoped: true` and a static
  `withheld_sections` list. Neither says whether anything was withheld.
- Unscoped callers get the pre-change Cypher and responses. The
  unscoped-visible changes are two extra projected columns, `uid` and
  `repo_id`, on the anchor-resolve `CALL {}` branches, and the P3-2 decode
  change under Known Residuals.

## Known Residuals

- Unscoped decode (review P3-2). `impactNodeIdentityList` now keeps an
  undecodable `nodes(path)` element as a zero identity instead of skipping it,
  so the explain route's hop pairing stays aligned. `PathHasNodes` needs an
  id, uid, or labels. This changes unscoped output only for elements the
  decoder cannot read.
- Scope-only grants (review P3-3). Grants are matched on repository ids. A
  token holding only ingestion scope ids sees empty answers on these three
  routes, which fails closed as the other impact routes do. `http-api.md`
  states it.
- Timing (review P3-4). An ungranted anchor costs one ownership statement
  (milliseconds) more than an unknown one. Response bytes are identical (T1).
- A scoped name with more than 32 carriers is judged over the first 32 in id
  order. A caller whose own node sorts later sees it as unknown.

## Where The Design Changed, And The Measurements That Changed It

Three points of the decided design did not hold on the code or the pinned
NornicDB build. Each change was measured before it was adopted. The
measurements in this section are historical and NornicDB-only. They ran on the
pinned NornicDB build before the owner's 2026-09-25 rule moved live tests and
timings to Neo4j. The Budget section below carries the current Neo4j figures.

1. **WorkloadInstance carries no `uid`.** `canonicalWorkloadInstanceUpsertCypher`
   (`go/internal/storage/cypher/canonical.go`) merges on `id` and sets no
   `uid`, and the only index is `nornicdb_workload_instance_id_lookup ON (i.id)`.
   A `wi.uid IN $uids` filter can never match, so the rescue statement filters
   on `wi.id`.
2. **The grant-anchored ownership statements do not scale, and one is
   unindexed.** `TerraformResource.repo_id` has no index, so
   `MATCH (t:TerraformResource {repo_id: g})` scans the label once per grant
   id. The measured fixture held 256 repositories, 2048 CloudResources, 2048
   TerraformStateResources, 2048 rescued WorkloadInstances, and 22048
   TerraformResources (20000 of them noise). Each figure is 2000 keys,
   uncached, with a unique `$nonce` per execution:

   | Grant-anchored shape | G=8, one statement | G=8, chunks of 500 | G=128, one statement | G=128, chunks of 500 |
   | --- | --- | --- | --- | --- |
   | A1 CloudResource | 1.028 s | 1.288 s | 7.754 s | 9.727 s |
   | R1b WorkloadInstance (on `wi.id`) | 0.925 s | 1.052 s | 6.385 s | 6.318 s |
   | R2 TerraformStateResource | 4.306 s | 11.582 s | 32.433 s | 105.577 s |

   With a scratch `TerraformResource(repo_id)` index, R2 at G=128 fell to
   4.1–4.5 s. That is still proportional to the grant, and it would be a
   schema change.
3. **Per-chunk cost is quadratic in the key list, and grant-free owner
   projections avoid the grant term entirely.** The shipped statements
   (`impact/ownership/statements.go`) anchor on the page's own keys
   (keyed node first, `WHERE n.<key> IN $uids`), walk the one owning edge, and
   return `DISTINCT uid, repo_id ORDER BY uid, repo_id LIMIT $row_limit`. The
   grant is applied in Go (`AllowsRepositoryID`). Chunk sweep, 2048 keys per
   class:

   | Chunk | CloudResource | WorkloadInstance | TerraformStateResource |
   | --- | --- | --- | --- |
   | 50 | 0.247 s | 0.241 s | 0.244 s |
   | 100 | 0.359 s | 0.388 s | 0.363 s |
   | 250 | 0.788 s | 0.845 s | 0.818 s |
   | 500 | 1.502 s | 1.592 s | 1.546 s |

   This curve also explains the ~9 s the brief recorded for R1a: that ran
   unchunked at 2000 keys. Starting the TerraformStateResource pattern from
   the TerraformResource side cost 6.6 s at chunk 500, so the keyed node goes
   first. Putting the grant back as a filter (`... AND wi.repo_id IN
   $grant_ids`) stayed exact but grew with the grant: 0.34 s at G=8, 0.74 s at
   G=128, and 3.7 s at G=1000 per class. It was rejected.

The shipped path, measured through `ownership.Checker` with the final
`ORDER BY ... LIMIT` statements and the key order rotated per round so no chunk
hits the build's read-result cache, is 2048 keys per class at chunk 50:

| Grant size | CloudResource | WorkloadInstance | TerraformStateResource | Misjudged keys |
| --- | --- | --- | --- | --- |
| 8 | 0.325 s | 0.319 s | 0.334 s | 0 |
| 128 | 0.292 s | 0.308 s | 0.294 s | 0 |
| 1000 | 0.290 s | 0.303 s | 0.304 s | 0 |

## Budget

The figures in this section were measured on Neo4j (`neo4j:2026-community`),
the live test backend since the owner's 2026-09-25 rule. Cost follows the
number of statement-checked keys and their owner fan-in, independent of grant
size, so the cap does not need to depend on the grant. On Neo4j the shipped
statements take 0.10–0.35 s per 2048 keys per class at grant 8, 128, and 1000,
with 0 misjudged keys. The rules are:

- `ChunkSize = 50`.
- `MaxCheckedKeys = 4500` distinct statement-checked keys per request. This is
  at least the largest page a route builds (trace-resource-to-code: 201 rows ×
  21 nodes), so an ordinary page is never capped.
- `RowLimit = 800` rows per chunk. That averages 16 owning repositories per
  key. A chunk that reaches it leaves its unadmitted keys unchecked.
- Unchecked keys are ungranted, and the verdict reports `Capped`, so the route
  reports `truncated: true`.
- The 10 s graph-read deadline (`neo4j_read_policy.go`) applies per
  statement.
- `RowLimit` caps the rows a chunk returns, not the owner edges the engine
  expands before `DISTINCT`, `ORDER BY`, and `LIMIT`. So the fan-in-1 figure
  is not a worst case. See the hub measurement below.

T7 (`TestLiveImpactOwnershipCapAndDeadline`) ran at a 1000-id grant over
6144 statement-checked keys:

- 4500 keys were checked and all judged owned, which is correct for this
  grant. The other 1644 were withheld. `Capped` was true. The check took
  0.252 s on Neo4j.
- A page of exactly 4500 keys was not capped.

Historical, NornicDB only, before the Neo4j rule: the same page took 0.745 s,
and fan-in-1 cost was about 0.15 ms per key (0.29–0.33 s per 2048 keys per
class, about 0.7 s at the cap).

### Hub fan-in (review finding B3)

`TestLiveImpactOwnershipHubFanIn` seeds 50 hub CloudResources, each `USES`d
by 2000 WorkloadInstances owned by 2000 distinct repositories, plus 4450
fan-in-1 CloudResources. It runs the shipped `CloudResourceOwnerCypher` on the
50 hub keys with a unique `$nonce` per run, then runs a 4500-key page holding
the 50 hubs through `Checker.Check` with rotated key order. The grant is the
repository that sorts last, so every hub's granted owner falls past
`RowLimit`.

| Backend | One 50-hub chunk, n=9 | 4500-key page with the hubs, n=9 | Hubs admitted |
| --- | --- | --- | --- |
| `neo4j:2026-community` (fresh container, hub fixture only) | median 0.036 s, max 0.428 s (cold first run) | median 0.736 s, max 1.359 s | 0 of 50, `Capped` every run |
| `neo4j:2026-community` (same container, full live suite loaded) | median 0.021 s, max 0.078 s | median 2.000 s, max 2.576 s | 0 of 50, `Capped` every run |
| historical: pinned NornicDB `fix-500-e022384c` | 12.340, 12.785, 13.826, 13.937, 14.167, 14.654 s (n=6, then stopped) | not measured | not measured |

The NornicDB row is historical. It was measured before the owner moved live
timing to Neo4j on 2026-09-25, and the run was stopped at n=6. The row is
NornicDB-only and is kept as observed, not as a Neo4j result.

What this shows:

- On Neo4j the worst case measured, 2.58 s for a full page of hubs, is inside
  the 10 s graph-read deadline. The bound holds without a statement change.
- Historical, NornicDB only: one hub chunk exceeded the 10 s deadline on the
  pinned build. On that backend a hub-heavy page fails the request closed with
  a graph-read deadline error. It never returns a partial or widened answer
  and never admits a node. The statements are not changed for it.
- The admit decision stays correct at a hub. A granted owner past `RowLimit`
  leaves the key unchecked, so ungranted with `Capped`, and the route reports
  `truncated`. It is never admitted (0 of 50 on every run). The hermetic
  `TestCheckRowLimitLeavesChunkUndecidedAndCapped` guards the same branch.

Running the class statements concurrently was not measured and is not done.
At 0.10–0.35 s per class there is nothing to recover inside the deadline, and three
concurrent statements per request would triple backend concurrency for no
bounded-latency gain. The classes run in sequence within one request, and
requests stay concurrent. No write, lock, or shared state is involved.

## A Backend Gap Found On The Way: The Exposure Walk Returns Nothing On NornicDB

On the pinned NornicDB build, `buildExposurePathCypher` returns no rows for any
caller, shared key included. `MATCH (reached {id:'x'}) MATCH
(reached)-[sinkRel]->(sinkNode) WHERE type(sinkRel) IN $rels` returns 0 rows.
The inline typed pattern `-[:EXECUTES_SHELL]->` returns the row.
`-[:CALLS*0..3]->` never yields the zero-length path, and it reports the
1-hop node at length 0. This predates this change and is tracked as #7177.

On Neo4j (`neo4j:2026-community`) the walk returns paths. There,
`TestLiveImpactScopedGrantTwoTenant` proves the scoped exposure filter end to
end: the shared-key walk reaches the fixture sinks, and both scoped callers
get exactly `[cr-a, sh-a]` with no repo-b identifier. On NornicDB the scoped
exposure filter is proven per class only, through the live ownership
statements, and has no live end-to-end path until #7177 lands. The public
docs (`http-api.md` and the exposure OpenAPI description) say the same.

## RED / GREEN

Every leak class was run RED on the unchanged handlers and then GREEN.

| Test | RED on the old code | GREEN |
| --- | --- | --- |
| T1 `TestScopedTraceResourceToCodeUngrantedAnchorIsUnknown` (id, name, orphan, Platform) | traversal issued, repo-b path returned | byte-identical to unknown, 0 traversals |
| T2 `...SharedResourceReturnsOnlyGrantedRepo` | `[repo-a repo-b]` | `[repo-a]` |
| T3/T5 `...DropsUngrantedInteriorPaths` | 9 paths incl. repo-b and foreign interiors | wi-a and wi-rescued paths only; A1 uids exactly `[cr-a cr-b cr-orphan]` |
| T3 `...TruncatedFromRawCount` | 2 rows, unfiltered | `truncated: true`, 1 row |
| T4 empty grant, all three routes | 2 / 3 / 1 graph calls | 0 |
| E1 `TestScopedExplainDependencyPathUngrantedEndpointIs404` | shortestPath issued, 200 | same 404, 0 shortestPath |
| E2 `...ForeignInteriorHasNoPath` | path returned | no path/depth/confidence/reason |
| E3 `...GrantedMatchesSharedKey` | no `scoped` field | equal to shared-key minus disclosure |
| E4 `...RescuedEndpointAdmitted` | (should-admit guard) | admitted |
| X1 `TestScopedTraceExposurePathForeignSourceIsNotFound` | `fn-b` by id walked | not found, 0 walks |
| X2–X5 `...FiltersPaths` | 6 sinks incl. cr-b, sh-shared, cidr | `[cr-a sh-a]`, reason names both withheld classes |
| X6 `...TruncatedFromRawCount` | 25 foreign-interior paths returned | 0 paths, `truncated: true` |
| D1 `TestScopedTraceResourceToCodeSharedNameResolvesGrantedNode` (a name two tenants share, foreign row first) | `start = {id: shared-handler}`: the caller's own node rendered as unknown (run on `d7f1e0276`) | repo-a's `fn-dup-2` on 20/20 runs, one traversal anchored on it |
| D1 `...UngrantedOnlyNameIsUnknown` | (guard) | byte-identical to an unknown name, 0 traversals |
| B1 `TestScopedTraceExposurePathCountsWithheldSinkClass` | `withheld_sink_class = 0`, the CidrBlock sink counted as `ungranted_node` | `withheld_sink_class = 1`, `ungranted_node = 3` |
| P3-1 `TestExposureOwnershipNodesReadsNestedProperties` | nested-`properties` node decoded to an empty id (fix reverse-applied) | id and repo_id decoded |
| Real middleware `TestAuthMiddlewareWithScopedTokensAdmitsImpactPathRoutes` | 403 on all three (run on `origin/main`) | handler reached; no repo-b identifier in any body |

The live runs (`ESHU_OCI_PROVE_LIVE=1`, pinned image
`ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-500-e022384c@sha256:74a8ed7b...`,
docker-compose env, fresh container per measurement run) are:

- `TestLiveImpactScopedGrantTwoTenant` (T6): 1-id and 130-id grants, with
  shared-key negative controls proving the leaks exist unfiltered.
- `TestLiveImpactOwnershipStatementCost`.
- `TestLiveImpactOwnershipCapAndDeadline` (T7).
- `TestLiveImpactOwnershipGrantFilterVariant` (the rejected variant).
- `TestLiveByIdImpactAnchorReads`, which is unchanged. It hard-codes the
  NornicDB database name `nornic`, so it does not run against Neo4j.
- `TestLiveImpactOwnershipHubFanIn` (B3, above).
- `TestLiveImpactScopedAnchorSharedName` (D1). Two tenants' CloudResources
  share a name, and repo-b's node sorts first by id. On 20 of 20 runs a scoped
  repo-a caller anchors on repo-a's node through the real handler and the live
  ownership statement. A name only repo-b carries renders as unknown.

The #5167 review round re-ran the whole live suite on `neo4j:2026-community`
(`ESHU_LIVE_GRAPH_BACKEND=neo4j`), per the owner's 2026-09-25 rule that live
tests and timings use Neo4j. Every test above passed there except
`TestLiveByIdImpactAnchorReads` (the database name). Shipped-statement cost
on Neo4j: 0.10–0.35 s per 2048 keys per class at grant 8, 128, and 1000, with
0 misjudged keys. The capped 6144-key page took 0.252 s.

Performance Evidence: impact scoped ownership check (impact/ownership), measured on neo4j:2026-community, uncached with a unique nonce per run, through ownership.Checker. The fixture has 256 repositories, 2048 CloudResources, 2048 TerraformStateResources, 2048 rescued WorkloadInstances and 22048 TerraformResources, with indexes as shipped by graph.EnsureSchemaWithBackend (backend neo4j). The shipped grant-free owner projections run at chunk 50, keyed node first, returning DISTINCT uid and repo_id with ORDER BY uid, repo_id and LIMIT 800. They take 0.10-0.35 s per class for 2048 keys at grant 8, 128 and 1000, with 0 misjudged keys. A 6144-key page at a 1000-id grant is capped at 4500 checked keys and finishes in 0.252 s. Hub fan-in (50 CloudResources x 2000 owners, n=9): one 50-hub chunk has a median of 0.036 s (max 0.428 s cold); a 4500-key page holding the hubs has a median of 0.736 s (max 1.359 s), and with the whole live suite's fixtures loaded a median of 2.000 s (max 2.576 s). No hub whose granted owner the row cap cut was admitted. The scoped anchor candidate resolve is the existing CALL{UNION} with ORDER BY id, label and LIMIT 32 on its RETURN clause, and it runs for scoped callers only. The scoped trace-resource-to-code traversal is the existing statement plus a terminal-repository grant in its anchoring WHERE and a nodes(path) projection. Unscoped statements are unchanged apart from two projected columns on the anchor-resolve CALL branches. Historical, NornicDB only, measured on the pinned image before the 2026-09-25 Neo4j rule: the grant-anchored A1/R1b/R2 shapes at grant 128 and 2000 keys took 7.754 s, 6.385 s and 32.433 s unchunked (R2 took 105.577 s in chunks of 500); the shipped projections took 0.29-0.33 s per class and 0.745 s for the capped page; and one 50-hub chunk took 12.3-14.7 s (n=6), past the 10 s per-statement deadline, where the request fails closed and admits nothing.

Observability Evidence: eshu_dp_query_impact_scoped_paths_withheld_total{route,reason} counts paths or answers withheld, where reason is ungranted_node, unchecked_over_cap, withheld_sink_class, or anchor_ungranted. eshu_dp_query_impact_ownership_check_duration_seconds{route,node_label,outcome} times every ownership chunk statement. A Warn log "impact ownership check capped" carries route, grant_size, and checked_key_cap when a page exceeds the budget. Both metrics are registered in go/internal/telemetry/instruments.go and have a coverage row in docs/public/observability/telemetry-coverage.md.

## Reproduce

The review round ran the same suite on Neo4j:

```bash
docker run -d --name neo4j-impact2 -p 17998:7687 -e NEO4J_AUTH=none neo4j:2026-community
cd go
ESHU_LIVE_GRAPH_BACKEND=neo4j ESHU_OCI_PROVE_LIVE=1 ESHU_NEO4J_URI=bolt://localhost:17998 \
  go test ./internal/query -run 'TestLiveImpact' -count=1 -v
docker rm -f neo4j-impact2
```

The historical NornicDB runs (before the 2026-09-25 Neo4j rule):

```bash
docker run -d --name nornic-5167impact --platform linux/amd64 -p 17998:7687 -p 17999:7474 \
  -e NORNICDB_NO_AUTH=true <compose env> <pinned image from docker-compose.yaml>
cd go
ESHU_OCI_PROVE_LIVE=1 ESHU_NEO4J_URI=bolt://localhost:17998 go test ./internal/query \
  -run 'TestLiveImpactScopedGrantTwoTenant|TestLiveImpactOwnershipStatementCost|TestLiveImpactOwnershipCapAndDeadline' \
  -count=1 -v
# the grant-anchored comparison table:
ESHU_IMPACT_OWNERSHIP_COMPARE=1 ESHU_OCI_PROVE_LIVE=1 ESHU_NEO4J_URI=bolt://localhost:17998 \
  go test ./internal/query -run TestLiveImpactOwnershipStatementCost -count=1 -v
docker rm -f nornic-5167impact
```

Two NornicDB behaviors surfaced while building these fixtures and are noted
for the next fixture author:

- `WITH n LIMIT k DETACH DELETE n` deletes nothing.
- One `DETACH DELETE` over ~35k nodes fails with "Txn is too big to fit into
  one request".

The fixture cleanup deletes by listed keys in batches.
