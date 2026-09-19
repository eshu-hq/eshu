# #6786: repository dependency marker and relationship repo-anchor fixes

Two NornicDB v1.3.3 correctness defects found during the epic #6788 prod
Cypher audit, fixed with TDD in `fix/6786-nornicdb-defect-exposure`.

## Defect 1: repository dependency marker (`is_dependency`)

`querycontract.RepositoryDependencyMarkerProjection` rendered
`EXISTS { MATCH (r)<-[:DEPENDS_ON]-(dep:Repository)... } as is_dependency`
as a per-row RETURN expression, consumed by `GET /api/v0/repositories`
(unscoped and scoped) and `GET /api/v0/catalog` (unscoped).

- On NornicDB v1.3.3 an `EXISTS` used as a RETURN expression is **always
  false**, so every repository row reported `is_dependency=false`
  regardless of ground truth.
- In scoped mode the caller's grant predicate was spliced directly after the
  `MATCH` pattern with no `WHERE` keyword
  (`MATCH (r)<-[:DEPENDS_ON]-(dep:Repository) AND (dep.id IN
  $allowed_repository_ids OR ...)`), which is invalid Cypher: Neo4j fails the
  whole scoped repository list with `Neo.ClientError.Statement.SyntaxError`,
  while NornicDB silently accepts it and still returns false.

Fix: `is_dependency` is now derived in Go
(`repository.repositoryDependencyTargetSet`) from the same bounded,
already-scoped `(:Repository)-[:DEPENDS_ON]->(:Repository)` edge pre-pass the
handler already ran for dependency-cluster grouping
(`repository.loadRepositoryDependencyEdges` /
`repositoryDependencyClusterEdgeCypher`, issue #3504), computed once and
reused for both signals. The page query's `RETURN` no longer carries any
`EXISTS`, `DEPENDS_ON`, or `is_dependency` text. `GET /api/v0/catalog`, which
never ran the edge pre-pass before, now runs it once (unscoped) to derive the
same marker.

## Defect 2: repo-filtered relationship lookup

`codequery` `relationshipsGraphRow`'s name+repo_id branch rendered
`e.name = $name AND EXISTS { MATCH (e)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(repo:Repository)
WHERE repo.id = $repo_id }` as a filter on a bare `MATCH (e)` label scan
(`codemodel.RelationshipGraphRowCypher`).

- On NornicDB v1.3.3 that backward multi-hop `EXISTS` is silently ignored: a
  same-named entity in a *different* repository still matched, and
  `RunSingle` returned whichever row came back first, regardless of the
  requested `repo_id`.
- Neo4j evaluates the `EXISTS` correctly.

Fix: `codemodel.RelationshipGraphRowCypherAnchored` lets the caller supply
the entity `MATCH` clause. The `repo_id` branch now anchors on
`MATCH (anchorRepo:Repository {id: $repo_id})-[:REPO_CONTAINS]->(anchorFile:File)-[:CONTAINS]->(e)`
— the same repository-anchored shape `entity.BuildResolveEntityGraphQuery`
already uses — instead of a post-hoc existence check. `RelationshipGraphRowCypher`
(the entity_id and name-only branches) is unchanged, implemented as
`RelationshipGraphRowCypherAnchored("MATCH (e)", predicate)`.

**Reachability correction (#6786 review F2):** the buggy branch runs inside
`relationshipsGraphRow` only when `CodeHandler.graphBackend()` is NOT
`GraphBackendNornicDB` -- NornicDB dispatches earlier, to
`nornicDBRelationshipsGraphRow` -> `relationships.MetadataRow`
(`identity.go`), a different, already-correct shape (two separate forward
`MATCH` clauses, not this backward `EXISTS`). In production,
`loadGraphBackend` resolves an unset `ESHU_GRAPH_BACKEND` to
`GraphBackendNornicDB` (`querycontract.ParseGraphBackend("")`) *before*
`CodeHandler` is constructed, and both `cmd/api` and `cmd/mcp-server` always
pass that resolved value -- never an empty string.
`CodeHandler.graphBackend()`'s own `"" -> GraphBackendNeo4j` fallback (a
*different* default than `ParseGraphBackend`'s) is therefore reachable only
from code that constructs `CodeHandler` directly without the config loader,
such as this defect's original live-test harness. **This defect was not
reachable on the default NornicDB dispatch; it is reachable only when an
operator explicitly sets `ESHU_GRAPH_BACKEND=neo4j` while the backing store
is actually NornicDB** (a mismatched configuration). The fix is still
correct and is defense-in-depth hardening for that path. A live subtest
(`nornicdb_dispatch`, below) proves the actual default path resolves both
repositories' entities correctly, live, both before and after this change
(it was never broken).

## RED/GREEN

Live tests behind `-tags live_nornicdb_answer_truth`:

- `go/internal/query/repository/nornicdb_dependency_marker_live_test.go`
  (`TestLiveRepositoryDependencyMarkerAnswerTruth`) drives the real
  `GET /api/v0/repositories` (unscoped, scoped) and `GET /api/v0/catalog`
  handlers.
- `go/internal/query/codequery/nornicdb_relationship_repo_anchor_live_test.go`
  (`TestLiveRelationshipRepoAnchorAnswerTruth`) drives the real
  `POST /api/v0/code/relationships` handler.

RED (before the fix), against isolated containers, NornicDB v1.3.3
(`timothyswt/nornicdb-cpu-bge:v1.3.3@sha256:81cedbf48898f4c37d05c325fee76b6d797b43e290e3a8a4e9eea936f0ec827f`)
and Neo4j (`neo4j:2026-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f`),
schema applied via `graph.EnsureSchemaWithBackend`:

- NornicDB, defect 1: unscoped and scoped subtests failed — every seeded
  repository reported `is_dependency=false` including the two true
  dependency targets.
- Neo4j, defect 1: unscoped passed (already correct); scoped subtest failed
  with `status = 500 ... Neo.ClientError.Statement.SyntaxError (Invalid
  input '{': ... EXISTS { MATCH (r)<-[:DEPENDS_ON]-(dep:Repository) AND
  (dep.id IN $allowed_repository_ids OR ...) }`.
- NornicDB, defect 2: querying `"Run"` scoped to repo A and repo B resolved
  to the *same* entity id, proving `repo_id` was not applied.
- Neo4j, defect 2: passed (already correct).

GREEN (after the fix), same containers, same seeds:

```
ESHU_NEO4J_URI=bolt://127.0.0.1:27900 ESHU_LIVE_GRAPH_BACKEND=nornicdb go test ./internal/query/repository -tags live_nornicdb_answer_truth -run TestLiveRepositoryDependencyMarkerAnswerTruth -count=1 -v
  => 4 passed (unscoped, scoped, catalog_unscoped)
ESHU_NEO4J_URI=bolt://127.0.0.1:27910 ESHU_LIVE_GRAPH_BACKEND=neo4j   go test ./internal/query/repository -tags live_nornicdb_answer_truth -run TestLiveRepositoryDependencyMarkerAnswerTruth -count=1 -v
  => 4 passed
ESHU_NEO4J_URI=bolt://127.0.0.1:27900 ESHU_LIVE_GRAPH_BACKEND=nornicdb go test ./internal/query/codequery  -tags live_nornicdb_answer_truth -run TestLiveRelationshipRepoAnchorAnswerTruth -count=1 -v
  => 3 passed (top level, neo4j_dispatch_hardening subtest, nornicdb_dispatch subtest)
ESHU_NEO4J_URI=bolt://127.0.0.1:27910 ESHU_LIVE_GRAPH_BACKEND=neo4j   go test ./internal/query/codequery  -tags live_nornicdb_answer_truth -run TestLiveRelationshipRepoAnchorAnswerTruth -count=1 -v
  => 2 passed (top level, neo4j_dispatch_hardening subtest; nornicdb_dispatch is skipped here -- see F2 above)
```

The `nornicdb_dispatch` subtest (#6786 review F2) drives the same two-repo
same-name seed through `CodeHandler{GraphBackend: GraphBackendNornicDB}`
against the live NornicDB container -- the actual production default path
(`nornicDBRelationshipsGraphRow` -> `relationships.MetadataRow`). It passes:
each repository's `Run` entity resolves to its own repository, confirming
this path was never affected and needed no fix.

Non-live unit coverage pins the new Cypher/Go shapes:
`code_relationships_graph_response_test.go`,
`relationship_repo_anchor_test.go`, `dependency_cluster_test.go` (new
`repositoryDependencyTargetSet`/`loadRepositoryDependencyEdges` cases,
`resolveRepositoryDependencyEvidence`, and the seeded RED/GREEN test for
the X4 AND/OR-after-whitespace guard via `querytestutil.AssertCypherHasNoBrokenAndOr`),
`list_dependency_marker_test.go` (rewritten per review F1 to assert
`truncated=false` for a degraded-but-complete page),
`catalog_dependency_marker_test.go` (same F1 assertion for the catalog, plus
the whole-graph pre-pass this evidence's Performance Evidence section
below explains was kept over a page-scoped alternative), and `authz_test.go`
(updated to reflect that `is_dependency` is now edge-derived, not a fake
row field).

`go test ./internal/query/... -count=1 -json` => 9631 test and subtest
results passed, 0 failed (after the rebase onto origin/main `59c605e48`,
including the #6800 probe integration).
`go vet ./internal/query/... ./internal/queryplan/...` => clean.
`gofumpt -l` on every changed file => no output.

## Performance Evidence

*(Rewritten for review finding F4: the original text below predated the
degradation-disclosure and truncation-detection work and understated the
current LIMIT. This paragraph reflects the branch after the rebase onto `59c605e48`.)*

No-Regression Evidence: this is a correctness fix, not a performance
optimization, so the acceptance bar is "no regression" on the touched
statements, not a scaled corpus replay. Single-observation-per-shape medians
(n=40 reads each, identical seed reused for before/after, same isolated
NornicDB v1.3.3 container/session pattern `repository/dependency_cluster.go`
already documents its own edge-cypher timing with), 20 repositories / 10
DEPENDS_ON edges for the repository-list shapes, 2 repositories / 1
same-named Function each for the relationship shape:

| Statement | Before | After |
| --- | --- | --- |
| Repository list page query (unscoped, is_dependency column) | 303.8µs median (n=40) | 283.1µs median (n=40) |
| Dependency-edge pre-pass (unchanged Cypher; new extra round trip for `GET /api/v0/catalog` only — `GET /api/v0/repositories` already ran it for clustering) | n/a | 225.2µs median (n=40) |
| Relationship repo-anchored lookup (`POST /api/v0/code/relationships`, name+repo_id) | 255.0µs median (n=40) | 237.5µs median (n=40) |

Both statements got marginally faster (dropping a per-row EXISTS subquery
and replacing a scan-then-filter with an anchored MATCH are both cheaper,
not more expensive) and are within run-to-run noise either way at this
corpus size — well under the skill's 10%/60s regression stop threshold in
either direction. `GET /api/v0/catalog`'s only new cost is the bounded edge
pre-pass it previously skipped entirely, which is required for correctness
(defect 1) and is the exact query `repository.loadRepositoryDependencyEdges`
runs -- bounded by `repositoryDependencyClusterEdgeFetchLimit` (**50001**,
one past `repositoryDependencyClusterEdgeLimit`'s 50000, added after this
table was first measured so truncation is detectable; see review F1) with
an `ORDER BY`. No change to result cardinality, page limits, or timeout
behavior on any touched handler. Absolute figures are resource-qualified to
this shared host and container image pinning; only the relative
before/after delta on identical inputs is the claim.

**Review finding F6 (catalog page-scoping, evaluated and rejected):** a
page-scoped alternative for the catalog's read
(`MATCH (s:Repository)-[:DEPENDS_ON]->(t:Repository) WHERE t.id IN $page_ids
RETURN DISTINCT t.id`, bounded by page size instead of the whole graph's
edge count) was measured live on NornicDB v1.3.3 at 5000 repositories /
20000 `DEPENDS_ON` edges, schema applied, isolated container. Its cold-path
cost scales ~linearly with `len($page_ids)` at roughly 55-60ms/element
(1 element: ~370ms; 10: ~885ms; 100: ~5.9s; 2000, the default catalog page:
1m55s-2m5s across two independent runs), and — critically — this cost is
**not cached by statement text, only by exact parameter VALUES**: a second,
different 2000-element `$page_ids` list on the identical already-warmed
statement paid the full ~2-minute cost again (1m59.99s measured). A real
catalog page's repository-id set drifts on ordinary repository churn, so in
production this would be paid on most requests, not once. This is worse
than the whole-graph pre-pass it would have replaced, which stays flat at
70-80ms warm / 250-500ms cold regardless of page size at this same corpus.
The catalog therefore keeps the whole-graph pre-pass (the row directly
above this one); the page-scoped shape was not committed. This looks like
its own NornicDB v1.3.3 performance defect (an `IN`-list evaluation whose
cold path is not statement-text-cached) distinct from the X1-X10
correctness shapes catalogued in
[6786-nornicdb-400-409-exposure.md](6786-nornicdb-400-409-exposure.md);
recording it here pending a decision on whether it warrants its own
upstream report.

**Rebase onto #6800 (DEPENDS_ON cardinality probe).** #6800 (issue #6794)
landed on `main` while this branch was open. It gated the same edge pre-pass
behind `MATCH ()-[r:DEPENDS_ON]->() RETURN count(r)` for unscoped callers. The
rebase keeps that probe inside `loadRepositoryDependencyEdges`
(`dependency_edge_unscoped.go`, formerly `dependency_edge_probe.go`), so the repository list and the unscoped catalog
both skip the scan when the graph has no `DEPENDS_ON` edges. A skipped read
reports `Skipped` rather than degraded: zero edges means `is_dependency` is
false everywhere, which is complete evidence and not an incomplete marker
(`TestLoadRepositoryDependencyEdgesSkipIsNotDegraded`). A probe error still
runs the scan. #6800's prepass tests now run against `loadRepositoryDependencyEdges`.
After the rebase, the live `TestLiveRepositoryDependencyMarkerAnswerTruth`
(unscoped, scoped, catalog_unscoped) and `TestLiveRelationshipRepoAnchorAnswerTruth`
pass on NornicDB v1.3.3 and Neo4j 2026. No-Regression Evidence: the probe is
#6800's measured statement, unchanged; on a graph with edges the scan is the
same statement this branch already measured above.

### Review finding R2-F6: the catalog read on a production-shape graph

The figures above (225µs; "flat at 70-80ms warm") came from seeds whose
Repository nodes had almost no other relationships. #6794 showed that the
per-edge read `MATCH (s:Repository)-[:DEPENDS_ON]->(t:Repository) RETURN s.id,
t.id ORDER BY ... LIMIT 50001` expands every Repository's full adjacency on
NornicDB, costing 5.3-6.4s on a production-scale graph. Before this branch
`GET /api/v0/catalog` issued no dependency-edge read, so that cost was new on
the catalog. The repository list already ran it for clustering.

Setup: throwaway containers, NornicDB `v1.3.3@sha256:81cedbf4...` (Bolt
127.0.0.1:27980) and Neo4j `2026-community@sha256:eabfbb04...` (27990, auth
off), Eshu schema applied with `graph.EnsureSchemaWithBackendStrict`. Seed,
verified by reading rows back rather than write counters: 500 `:Repository`,
100,000 `REPO_CONTAINS`→`:File` (200 per repository), 300,000
`CONTAINS`→`:Function`, 500 `:Workload` (`DEFINES` from 100 repositories),
2,000 Workload→Workload `DEPENDS_ON` on Neo4j and 2,017 on NornicDB, and 300
Repository→Repository `DEPENDS_ON` on both. The extra 17 came from an
interrupted batched seed and only add Workload edges, which every correct
shape excludes. A nonce write preceded every timed call so no result cache
could answer it, with one warm-up and then interleaved rounds (5 on NornicDB,
7 on Neo4j), reported as medians.

Row-set proof: each shape's `(source, target)` list compared with the per-edge
read (A) at `LIMIT` 50001, 100 and 7. Grouped rows were flattened, sorted and
clipped in Go, as `flattenGroupedRepositoryDependencyEdges` does.

| Shape | NornicDB median | NornicDB rows = A | Neo4j median | Neo4j rows = A |
| --- | --- | --- | --- | --- |
| A: `(s:Repository)-[:DEPENDS_ON]->(t:Repository) RETURN s.id, t.id` | 0.6347s | reference (300) | 0.0050s | reference (300) |
| `(s)-[r:DEPENDS_ON]->(t) WHERE s:Repository AND t:Repository` | 4.4858s | **no**: 2,317 rows, label filter ignored | 0.0058s | yes |
| `()-[r:DEPENDS_ON]->()` + `WITH startNode(r), endNode(r) WHERE` labels | 4.4341s | yes | 0.0068s | yes |
| `()-[r:DEPENDS_ON]->()` + `labels()` filtered in Go | 8.9592s | **no** | 0.0250s | no at LIMIT 7 |
| `(s)-[r:DEPENDS_ON]->(t)` + `labels()` filtered in Go | 9.1546s | no at LIMIT 7 | 0.0248s | no at LIMIT 7 |
| `'Repository' IN labels(s) AND ...` | 4.7772s | **no**: 2,317 rows | 0.0074s | yes |
| `(t:Repository)<-[:DEPENDS_ON]-(s:Repository)` | 0.0090s | yes | 0.0053s | yes |
| **Grouped: `RETURN s.id AS source_id, collect(t.id) AS target_ids ORDER BY source_id`** | **0.0045s** | **yes** | **0.0047s** | **yes** |

Filtering in Go breaks truncation, because the Cypher `LIMIT` applies before
the label filter. The target-anchored shape is fast here only because these
seeded repositories have almost no incoming edges, which production does not
guarantee. The grouped shape matches NornicDB v1.3.3's
relationship-aggregation fast path (`pkg/cypher/traversal_fast_agg.go`,
`tryFastSingleHopAgg`, commit `a9956536`): a 1-hop typed pattern with no
`WHERE` and a `RETURN start.prop, collect(end.prop)` projection is answered
from `GetEdgesByType` plus a batch label check, so its cost follows the
`DEPENDS_ON` edge count, not repository adjacency.

Change: unscoped callers (the catalog, and shared/admin/local repository
lists) now use `RepositoryDependencyGroupedEdgeCypher`, still gated by the
#6800 probe. Scoped callers keep the per-edge read, because their grant is a
`WHERE` predicate that disables the fast path. The live
`TestLiveRepositoryDependencyMarkerAnswerTruth` scoped case passes on both
backends with that shape.

Performance Evidence: `listCatalogRepositoriesFromGraph(ctx, 1000)` on the
seeded graph, one warm-up and 5 rounds per build, two interleaved passes,
medians. Builds: base `59c605e48` (per-row `EXISTS`), this branch before the
change `fafd85714` (per-edge read), and after `b75dfbae4` (grouped read).

| Backend | Base 59c605e48 | Per-edge fafd85714 | Grouped b75dfbae4 | is_dependency true (truth 215) |
| --- | --- | --- | --- | --- |
| NornicDB v1.3.3 | 0.0564s, 0.0450s | 0.6585s, 0.5965s | 0.0126s, 0.0131s | base 0, per-edge 215, grouped 215 |
| Neo4j 2026 | 0.0066s, 0.0060s | 0.0114s, 0.0102s | 0.0117s, 0.0105s | 215 in all three |

On NornicDB the correct catalog is now faster than the incorrect base, because
the grouped read costs less than the per-row `EXISTS` it replaced. On Neo4j the
probe and the grouped read add about 5ms, which is what the correctness fix
costs there. The grouped read also replaces the per-edge read for unscoped
`GET /api/v0/repositories`, so #6794's remaining cost on graphs that do have
`DEPENDS_ON` edges drops for those callers too. Scoped repository lists still
pay the per-edge expansion. Those absolute figures apply only to this shared
host; the claim is the relative change on identical inputs.

The Neo4j `PROFILE` gate for the new `QP-REPOSITORY-DEPENDS-ON-GROUPED-EDGES`
entry passes (`TestProductionQueryplanProfilesRejectWholeGraphScans`, tag
`queryplan_profile_live`, against the Neo4j container above).

### Review finding R3-F2: bounding the grouped read's transfer

The grouped read's `LIMIT` bounds source groups, not edges. With fewer than
50,001 source repositories, which is every realistic graph, each unscoped
`/catalog` and `/repositories` request transferred and sorted every
Repository `DEPENDS_ON` edge, then clipped to 50,000. The per-edge read it
replaced stopped at 50,001 rows.

Fix: `loadUnscopedRepositoryDependencyEdges` bounds the transfer in every
case, using the #6800 probe count it already had.

- Probe count 0: no read (unchanged).
- Probe count at most 50,000: Repository edges are a subset of all
  `DEPENDS_ON` edges, so they fit. The grouped read runs as before with
  `$group_limit` = 50,001.
- Probe count above 50,000, or unreadable: `RepositoryDependencyGroupSizeCypher`
  (`RETURN s.id AS source_id, count(t) AS target_count ORDER BY source_id
  LIMIT 50001`, the same WHERE-free fast-path shape with `count` instead of
  `collect`) runs first. `repositoryDependencyGroupLimit` picks the smallest
  source prefix whose sizes sum past 50,000, and the grouped read fetches only
  that prefix. Because groups arrive in source order, the prefix holds the
  first 50,000 edges in (source, target) order, so the clipped list is
  unchanged. The read is marked truncated whenever the sizes prove more than
  50,000 edges, even if fewer ids come back (edges removed between the two
  reads, or null ids that `collect` drops). This also closes R3-P3-4: a group
  with only null ids can no longer hide truncation. The completion event gains
  `edge_transfer_capped`.

Setup: throwaway containers on this host, NornicDB
`v1.3.3@sha256:81cedbf4...` (Bolt 127.0.0.1:28040) and Neo4j
`2026-community@sha256:eabfbb04...` (28050, auth off), with the Eshu schema
applied through `graph.EnsureSchemaWithBackendStrict`. The seed had 500
`:Repository` nodes, 500 `:Workload` nodes and 2,000 Workload→Workload
`DEPENDS_ON` edges. Repository→Repository `DEPENDS_ON` edges ran i→i+d over a
range of d. All counts were read back as rows and matched the probe count on
both backends. A nonce write preceded every timed call. Medians are over 5
rounds after one warm-up (7 rounds for the loader comparison).

Scaling curve for R3-P3-5, with 500 Repository `DEPENDS_ON` edges and a
growing file count per repository (statement medians):

| Files per repo (REPO_CONTAINS / functions) | NornicDB per-edge | NornicDB grouped | Neo4j per-edge | Neo4j grouped |
| --- | --- | --- | --- | --- |
| 0 (0 / 0) | 0.0044s | 0.0071s | 0.0073s | 0.0083s |
| 100 (50,000 / 150,000) | 0.3511s | 0.0085s | 0.0136s | 0.0103s |
| 200 (100,000 / 300,000) | 1.2609s | 0.0154s | 0.0106s | 0.0091s |

On NornicDB the per-edge read grows with adjacency (0.004s to 1.26s). The
grouped read stays within about 10ms across the same growth, which is what
the `tryFastSingleHopAgg` mechanism predicts.

Edge-density curve at 200 files per repository. "Transferred" means edge ids
crossing the wire. The equality column compares the flattened and clipped
grouped edges with the per-edge read at LIMIT 50001.

| Repository edges (probe count) | Path | NornicDB: unbounded grouped / sizes + capped grouped | Neo4j: unbounded grouped / sizes + capped grouped | Transferred before → after | Equal to per-edge |
| --- | --- | --- | --- | --- | --- |
| 500 (2,500) | uncapped | 0.0154s / n/a | 0.0091s / n/a | 500 → 500 | yes |
| 40,000 (42,000) | uncapped | 0.0484s / n/a | 0.0690s / n/a | 40,000 → 40,000 | yes |
| 75,000 (77,000) | capped (334 of 500 groups) | 0.0804s / 0.0587s + 0.0766s | 0.1287s / 0.0408s + 0.0725s | 75,000 → 50,100 | yes (both) |
| 150,000 (152,000) | capped (167 of 500 groups) | 0.1028s / 0.0732s + 0.0997s | 0.2175s / 0.0803s + 0.0681s | 150,000 → 50,100 | yes (both) |

For reference, the per-edge read cost 2.20s, 3.23s and 4.35s on NornicDB at
40,000, 75,000 and 150,000 edges.

Performance Evidence: the end-to-end unscoped loader, compared with the
pre-change loader (probe, then the grouped read at `LIMIT 50001`, then
flatten). Calls were interleaved in the same process, with 7 rounds. The
40,000, 75,000 and 500 rows were measured after stepping the density down with
deletes. The NornicDB 40,000 row was re-measured on a fresh seed at 15 rounds,
over two passes.

| Repository edges | NornicDB before → after | Neo4j before → after | Edges / truncated (both builds) |
| --- | --- | --- | --- |
| 500 | 0.0073s → 0.0076s | 0.0057s → 0.0052s | 500 / false |
| 40,000 | 0.0423s → 0.0410s, 0.0388s → 0.0394s | 0.1195s → 0.0935s | 40,000 / false |
| 75,000 | 0.1602s → 0.1688s | 0.1707s → 0.1383s | 50,000 / true |
| 150,000 | 0.1538s → 0.1664s | 0.1889s → 0.0997s | 50,000 / true |

No-Regression Evidence: at or below the bound the statements are the same as
before, apart from the parameterized `LIMIT`. The differences are within
run-to-run noise. A first 7-round NornicDB pass at 40,000 edges, taken after
deletes, read 0.077s → 0.089s with overlapping samples. The fresh-seed
15-round re-run above shows no difference. Above the bound, where the response
is already degraded and truncated, the cap costs about 5-8% on NornicDB.
NornicDB walks the `DEPENDS_ON` index once per statement, and the extra
group-size statement costs more than the saved transfer at these sizes. On
Neo4j the cap is 19-47% faster. On both backends it bounds per-request memory
and wire transfer at 50,000 edges plus one repository's edges, instead of
growing with the whole Repository `DEPENDS_ON` set. The absolute figures apply
only to this shared host; the claim is the relative change on identical
inputs.

Observability Evidence: `edge_transfer_capped` on the
`repository_query.stage_completed` event for `stage=dependency_cluster_edges`
(both routes) shows when a request took the capped path. Its
`duration_seconds` includes the extra statement
(`TestDependencyEdgeStageCompletionAttributes`).

### Review finding R3-F3: live proof over non-Repository DEPENDS_ON edges

The unscoped answer now depends on NornicDB's relationship-aggregation fast
path checking both endpoint labels. The same pin ignores a label predicate in
`WHERE`, returning 2,317 rows against a true 300 (table above). The committed
live seed held only Repository→Repository edges, so it could not catch a pin
that stopped checking labels.

`TestLiveRepositoryDependencyMarkerAnswerTruth` now seeds Workload→Workload
(w1→w2), Repository→Workload (r1→w1) and Workload→Repository (w2→r3)
`DEPENDS_ON` edges next to the two Repository edges. Ground truth is
unchanged. The test asserts cluster membership (`group_key` for
`group_source=dependency_cluster`: {r1, r2} and {r3, r4}) as well as
`is_dependency`. A new subtest runs the two production grouped statements,
`readGroupedRepositoryDependencyEdges` and `readRepositoryDependencyGroupSizes`,
and fails on any row for this seed that carries a Workload endpoint. The
group-size statement is the one the capped path runs, and a small live seed
cannot reach it through the handler. The shared live reader and retry helpers
moved to `live_reader_test.go`.

RED, seeded: with the endpoint labels temporarily removed from both
production grouped statements (the shape a skipped label check produces),
NornicDB v1.3.3 fails `unscoped` (r3 `is_dependency=true`, and all four
repositories in one cluster keyed r1), `catalog_unscoped` (r3 true) and the
grouped-reads subtest (Workload ids in both reads). `scoped` still passes,
because it uses the per-edge read. GREEN at the final head: on fresh
containers (NornicDB `v1.3.3@sha256:81cedbf4...` on 28040, Neo4j
`2026-community@sha256:eabfbb04...` on 28050), all four subtests pass on both
backends, as does `TestLiveRelationshipRepoAnchorAnswerTruth`.

## Observability Evidence

*(Rewritten for review finding F4; current as of the rebase onto `59c605e48`.)*

The repository list's existing `repository_query.stage_started` /
`repository_query.stage_completed` log events for
`operation=repository_list, stage=dependency_cluster_edges` are unchanged in
shape and now also carry `edge_count`, `truncated`, and `error`
(previously only `cluster_count`), giving operators direct visibility into
how many `DEPENDS_ON` edges backed both the cluster grouping and the
`is_dependency` marker, and whether that read was itself degraded.

A failed or truncated dependency-edge read now additionally emits a
dedicated structured warning, `repository_query.dependency_edges_degraded`
(`logRepositoryDependencyEdgesDegradation`), with `operation`, `edge_count`,
`truncated`, and `error` attributes, for both `GET /api/v0/repositories`
(`operation=repository_list`) and `GET /api/v0/catalog`
(`operation=catalog_list`). This is new since the original version of this
section, which claimed "no new signal was needed" -- that was inaccurate
even at the time this fix first shipped disclosure at all (review F1
corrected what the disclosure could claim, not whether one existed).

The degraded read never fails the request (see
`logRepositoryDependencyEdgesDegradation`'s doc comment for why): it
discloses via `partial_reasons: ["dependency_marker_evidence_incomplete"]`
on the repository list and `limitations` on the catalog, and explicitly
does **not** set `truncated` / `result_limits.truncated` /
`repository_inventory_truncated` -- those mean "more repositories exist
beyond this page," which a degraded auxiliary read has no bearing on
(review F1). Both routes also still flow through the shared
`WriteGraphReadError` / bounded-read-error telemetry path for a failure in
their *primary* (non-dependency) reads, unchanged by this work.

Observability Evidence (review finding R2-F10): the catalog's dependency-edge
read is now timed with the same stage timer as the repository list,
`repository_query.stage_started` / `repository_query.stage_completed` with
`operation=catalog_list` and `stage=dependency_cluster_edges`. The completion
event carries `duration_seconds`, `edge_count`, `truncated`, `error` and
`edge_scan_skipped` (and, since R3-F2, `edge_transfer_capped`), but no
`cluster_count`, because the catalog builds no clusters
(`TestDependencyEdgeStageCompletionAttributes`, which covers both routes). Before this, the only
catalog signal for the read was the degraded-read warning, so its duration was
invisible. The contract is documented in
`docs/public/reference/http-api/catalog-workload-selection.md`.
