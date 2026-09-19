# #6786: capping the repository dependency-edge transfer

Round-3 review findings for the #6786 repository dependency marker work. This
continues
[6786-repository-dependency-marker-and-relationship-repo-anchor.md](6786-repository-dependency-marker-and-relationship-repo-anchor.md),
which covers the defect fixes, the R2-F6 grouped read, and the stage
telemetry.

## R3-F2: bounding the grouped read's transfer

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
  with only null ids can no longer hide truncation. When the sizes fit the
  bound, the grouped read runs at `$group_limit` = 50,001, the same as the
  uncapped case (R4-F1 below). The completion event gains
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
15-round re-run above shows no difference. When the Repository edges
themselves exceed the bound (the 75,000 and 150,000 rows), the response is
truncated and the cap costs about 5-8% on NornicDB.
NornicDB walks the `DEPENDS_ON` index once per statement, and the extra
group-size statement costs more than the saved transfer at these sizes. On
Neo4j the cap is 19-47% faster. On both backends it bounds per-request memory
and wire transfer at 50,000 edges plus one repository's edges, instead of
growing with the whole Repository `DEPENDS_ON` set. The absolute figures apply
only to this shared host; the claim is the relative change on identical
inputs.

The capped path is not limited to truncated responses. The trigger is the
whole-graph probe, which counts every `DEPENDS_ON` edge including Workload
edges. A graph with more than 50,000 Workload `DEPENDS_ON` edges and few
Repository edges takes the capped path on every unscoped request, even though
its answer is complete. R4-F3 below measures that regime.

Observability Evidence: `edge_transfer_capped` on the
`repository_query.stage_completed` event for `stage=dependency_cluster_edges`
(both routes) shows when a request took the capped path. Its
`duration_seconds` includes the extra statement
(`TestDependencyEdgeStageCompletionAttributes`).

## R3-F3: live proof over non-Repository DEPENDS_ON edges

The unscoped answer now depends on NornicDB's relationship-aggregation fast
path checking both endpoint labels. The same pin ignores a label predicate in
`WHERE`, returning 2,317 rows against a true 300 (see the R2-F6 table in
[6786-repository-dependency-marker-and-relationship-repo-anchor.md](6786-repository-dependency-marker-and-relationship-repo-anchor.md)). The committed
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

## R4-F1: a group added between the two capped reads

Round 4 found that the capped path, when the group sizes fit the bound, passed
the group count the size read saw as `$group_limit`. If a source repository
gained its first `DEPENDS_ON` edge between the size read and the grouped read,
and its id sorted inside the counted groups, the grouped read returned the new
group and pushed the last real group off the end. The flattened result fitted
the bound, so the response reported a complete answer that no single graph
state would produce.

Fix: when the sizes fit, the grouped read runs at the fetch limit (50,001),
not at `len(sizes)`. The grouped read then either returns every group or
returns more than 50,000 groups or edges, which
`flattenGroupedRepositoryDependencyEdges` reports as truncated. The sizes
proved at most 50,000 Repository edges a moment earlier, so the transfer stays
at the same bound as the uncapped case. When the sizes prove the bound is
exceeded, the prefix limit is unchanged and truncation is still reported.

- Unit RED/GREEN:
  `TestLoadUnscopedRepositoryDependencyEdgesCappedDetectsConcurrentGrowth`.
  The size read sees sources [a, b, z]. The grouped read sees [a, b, m, z] and
  applies `LIMIT $group_limit`. Before the fix, both cases returned [a, b, m]
  with `truncated=false`. After the fix, bound 5 returns all four edges, not
  truncated, and bound 3 returns three edges, truncated.
- Live RED/GREEN: `TestLiveRepositoryDependencyEdgesCappedConcurrentGrowth`
  (`dependency_edge_growth_live_test.go`) wraps the live reader so a real
  `CREATE` of m's edge lands on the backend right after the size read returns.
  Four Workload edges push the probe above the bound, so the capped path runs.
  It was run on fresh containers: NornicDB `v1.3.3@sha256:81cedbf4...` on
  127.0.0.1:28080 and Neo4j `2026-community@sha256:eabfbb04...` on 28090, with
  the schema applied.
  - With the `if !over` line removed, both backends fail both subtests.
    Bound 5 returns a, b, m, drops z and reports `truncated=false`. Bound 3
    returns a, b, m and reports `truncated=false`.
  - With the line restored, both subtests pass on both backends.

## R4-F3: capped-path cost when Workload edges trigger it

Round 4 found that the regime production is most likely to hit had not been
measured: a whole-graph `DEPENDS_ON` count above 50,000 because of Workload
edges, with a complete, non-truncated Repository answer. The sentence above
also said the capped path runs only on degraded responses; it is now
corrected.

Setup: fresh containers, NornicDB `v1.3.3@sha256:81cedbf4...` (Bolt
127.0.0.1:28080) and Neo4j `2026-community@sha256:eabfbb04...` (28090, auth
off). The Eshu schema was applied with `graph.EnsureSchemaWithBackendStrict`
at Eshu head `45a1c33aa`.

Seed:
- 500 `:Repository` nodes, each with 200 `REPO_CONTAINS` files and 3 functions
  per file (100,000 files, 300,000 functions).
- 2,500 `:Workload` nodes, 5 per repository via `DEFINES`.
- 62,000 random Workload→Workload `DEPENDS_ON` edges.
- Random Repository→Repository `DEPENDS_ON` edges stepped 300 → 5,000 →
  40,000 by adding edges.
- Every count was read back as rows and matched the probe (62,300, 67,000 and
  102,000).

Method: the head loader (`loadUnscopedRepositoryDependencyEdges`: probe, group
sizes, grouped read) was interleaved with the uncapped loader (probe, grouped
read at `LIMIT 50001`, flatten) and each statement alone. A nonce write
preceded every call. Medians are over 9 rounds after one warm-up.

At every step, both loaders returned identical edge lists, with
`truncated=false` and the head read `TransferCapped=true`.

| Repository edges (probe) | NornicDB uncapped → head | NornicDB probe / sizes / grouped | Neo4j uncapped → head | Neo4j probe / sizes / grouped |
| --- | --- | --- | --- | --- |
| 300 (62,300) | 0.0635s → 0.1053s | 0.0219s / 0.0402s / 0.0420s | 0.0056s → 0.0109s | 0.0010s / 0.0045s / 0.0043s |
| 5,000 (67,000) | 0.0694s → 0.1226s | 0.0219s / 0.0466s / 0.0572s | 0.0142s → 0.0237s | 0.0009s / 0.0078s / 0.0112s |
| 40,000 (102,000) | 0.2008s → 0.2742s | 0.0463s / 0.1025s / 0.1189s | 0.0914s → 0.1172s | 0.0014s / 0.0275s / 0.0558s |

In this regime the cap adds the group-size statement to a complete answer.
That costs 0.04-0.07s per unscoped request on NornicDB (+37-77%) and
0.005-0.026s on Neo4j (+28-95%). The size statement walks the same
`DEPENDS_ON` relationship-type index as the grouped read, Workload edges
included, so it costs about as much as the grouped read itself.

For comparison, `main` answered this route with the per-edge read, which cost
0.6s at 300 edges and 2.2s at 40,000 on NornicDB at this adjacency. Those
figures come from R2-F6 and the R3-F2 reference line, which were measured with
2,000 Workload edges. The branch is still a net improvement over `main`
in this regime; it is only slower than an uncapped grouped read.

Disproven alternative: gate the cap on a Repository-only count so this regime
skips the size statement. Measured on the same NornicDB seed at 300
Repository edges (median of 9):

| Statement | Count returned | Median |
| --- | --- | --- |
| Whole-graph probe | 62,300 | 0.017s |
| `MATCH (:Repository)-[r:DEPENDS_ON]->(:Repository) RETURN count(r)` | 300 | 1.009s |
| `MATCH (s:Repository)-[:DEPENDS_ON]->(t:Repository) RETURN count(t)` | 300 | 1.104s |
| `RETURN count(*)` over the same pattern | 300 | 1.014s |

On Neo4j every shape took about 2ms. NornicDB v1.3.3 has no fast path for a
label-anchored global count, and it costs about 25 times the size statement it
would replace. The size statement is already the cheapest Repository-only
count on the pinned NornicDB. Removing it would drop the transfer bound for
graphs with more than 50,000 Repository edges. The cost is therefore kept and
disclosed: `edge_transfer_capped=true` on the completion event, with the
extra statement included in its `duration_seconds`, lets an operator see when
this regime applies.

## Round-3 P3 dispositions

- **R3-P3-1 (X3 residual on the typed probe):** a negative probe count is now
  treated as unknown and takes the capped path. A residual that nets to
  exactly zero is added to the #6787 next-pin re-proof list in
  [6786-nornicdb-400-409-exposure.md](6786-nornicdb-400-409-exposure.md).
- **R3-P3-2 (`catalog.go` at 496 lines):** the catalog repository reads moved,
  unchanged, to `catalog_page.go` (first named `catalog_repositories.go`,
  renamed for the round-4 stutter finding R4-F2); `catalog.go` is now 405
  lines.
- **R3-P3-3 (no NornicDB plan check):** NornicDB v1.3.3 cannot provide one.
  Its EXPLAIN/PROFILE builds a static clause-order operator tree
  (`pkg/cypher/explain.go`, `analyzeQuery`) that never consults
  `tryFastRelationshipAggregations`, and PROFILE re-executes without
  parameters. Probed live on the pinned image: PROFILE of the grouped read
  returns no plan over Bolt. The fast-path property is guarded by the shape
  pins, the live label check (R3-F3) and the adjacency scaling curve (R3-F2).
  The hot-cypher caveats for both grouped entries say so. The Neo4j PROFILE
  gate now accepts either bounded anchor the planner picks for the grouped
  entries: `NodeByLabelScan` on a populated graph, and
  `DirectedRelationshipTypeScan` on a near-empty one. It failed on a fresh
  container before this change.
- **R3-P3-4 (all-null target ids):** the flatten doc states the invariant, and
  a table case pins that such a group still counts toward the group bound. The
  capped path reports truncation whenever the sizes prove the bound is
  exceeded.
- **R3-P3-5 (no scaling curve):** see the adjacency and edge-density curves
  under R3-F2.
- **R3-P3-6:** `dependency_cluster_test.go` is split (400 + 146 lines), and the
  #6794 evidence gains a naming note.
