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
- Unscoped callers get the pre-change Cypher and responses. The only
  unscoped-visible change is two extra projected columns, `uid` and
  `repo_id`, on the anchor-resolve `CALL {}` branches.

## Where The Design Changed, And The Measurements That Changed It

Three points of the decided design did not hold on the code or the pinned
NornicDB build. Each change was measured before it was adopted.

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

Cost is about 0.15 ms per statement-checked key plus owner fan-in,
independent of grant size, so the cap does not need to depend on the grant.
The rules are:

- `ChunkSize = 50`.
- `MaxCheckedKeys = 4500` distinct statement-checked keys per request. This is
  at least the largest page a route builds (trace-resource-to-code: 201 rows ×
  21 nodes), so an ordinary page is never capped.
- `RowLimit = 800` rows per chunk. That averages 16 owning repositories per
  key. A chunk that reaches it leaves its unadmitted keys unchecked.
- Unchecked keys are ungranted, and the verdict reports `Capped`, so the route
  reports `truncated: true`.
- Worst case: 4500 keys × ~0.15 ms ≈ 0.7 s. The 10 s graph-read deadline
  (`neo4j_read_policy.go`) applies per statement, and each chunk statement
  measured ~6 ms.

T7 (`TestLiveImpactOwnershipCapAndDeadline`) ran at a 1000-id grant over
6144 statement-checked keys:

- 4500 keys were checked and all judged owned, which is correct for this
  grant. The other 1644 were withheld. `Capped` was true. The check took
  0.745 s.
- A page of exactly 4500 keys was not capped.

Running the class statements concurrently was not measured and is not done.
At ~0.3 s per class there is nothing to recover inside the deadline, and three
concurrent statements per request would triple backend concurrency for no
bounded-latency gain. The classes run in sequence within one request, and
requests stay concurrent. No write, lock, or shared state is involved.

## A Backend Gap Found On The Way: The Exposure Walk Returns Nothing

On the pinned NornicDB build, `buildExposurePathCypher` returns no rows even
for a shared-key caller. `MATCH (reached {id:'x'}) MATCH
(reached)-[sinkRel]->(sinkNode) WHERE type(sinkRel) IN $rels` returns 0 rows.
The inline typed pattern `-[:EXECUTES_SHELL]->` returns the row.
`-[:CALLS*0..3]->` never yields the zero-length path, and it reports the
1-hop node at length 0. This predates this change and was reported on #5167.

Consequently the scoped exposure filter is proven with unit fakes (X2–X6) and,
on the live engine, by judging each sink and chain class through the live
ownership statements. The walk itself is not re-shaped here.

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
| Real middleware `TestAuthMiddlewareWithScopedTokensAdmitsImpactPathRoutes` | 403 on all three (run on `origin/main`) | handler reached; no repo-b identifier in any body |

The live runs (`ESHU_OCI_PROVE_LIVE=1`, pinned image
`ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-500-e022384c@sha256:74a8ed7b...`,
docker-compose env, fresh container per measurement run) are:

- `TestLiveImpactScopedGrantTwoTenant` (T6): 1-id and 130-id grants, with
  shared-key negative controls proving the leaks exist unfiltered.
- `TestLiveImpactOwnershipStatementCost`.
- `TestLiveImpactOwnershipCapAndDeadline` (T7).
- `TestLiveImpactOwnershipGrantFilterVariant` (the rejected variant).
- `TestLiveByIdImpactAnchorReads`, which is unchanged and still green.

Performance Evidence: impact scoped ownership check (impact/ownership) on the pinned NornicDB image, uncached, fixture of 256 repositories, 2048 CloudResources, 2048 TerraformStateResources, 2048 rescued WorkloadInstances, and 22048 TerraformResources, with indexes as shipped by graph.EnsureSchemaWithBackend (nornicdb_cloud_resource_uid_lookup, terraform_state_resource_uid_unique, nornicdb_workload_instance_id_lookup; TerraformResource.repo_id unindexed). Before, the grant-anchored A1/R1b/R2 shapes at grant 128 and 2000 keys took 7.754 s, 6.385 s, and 32.433 s unchunked, and R2 took 105.577 s in chunks of 500. After, the grant-free owner projections at chunk 50, keyed node first, returning DISTINCT uid and repo_id with ORDER BY uid, repo_id and LIMIT 800, take 0.29-0.33 s per class for 2048 keys at grant 8, 128, and 1000, with 0 misjudged keys. A 6144-key page at a 1000-id grant is capped at 4500 checked keys and finishes in 0.745 s. The scoped trace-resource-to-code traversal is the existing statement plus a terminal-repository grant in its anchoring WHERE and a nodes(path) projection. Unscoped statements are unchanged apart from two projected columns on the anchor-resolve CALL branches.

Observability Evidence: eshu_dp_query_impact_scoped_paths_withheld_total{route,reason} counts paths or answers withheld, where reason is ungranted_node, unchecked_over_cap, withheld_sink_class, or anchor_ungranted. eshu_dp_query_impact_ownership_check_duration_seconds{route,node_label,outcome} times every ownership chunk statement. A Warn log "impact ownership check capped" carries route, grant_size, and checked_key_cap when a page exceeds the budget. Both metrics are registered in go/internal/telemetry/instruments.go and have a coverage row in docs/public/observability/telemetry-coverage.md.

## Reproduce

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
